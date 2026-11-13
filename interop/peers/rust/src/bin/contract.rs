#[path = "../contract_engine.rs"]
mod contract_engine;
use agent_client_protocol::{
    AcpAgent, AcpAgentConfig, Agent, Client, Error, Stdio, UntypedMessage,
};
use contract_engine::{Engine, matches, validate_wire};
use serde_json::{Value, json};
use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};
fn read(path: &str) -> Value {
    serde_json::from_str(&std::fs::read_to_string(path).expect("fixture file"))
        .expect("fixture JSON")
}
macro_rules! run_profile {
 ($profile:ident,$version:expr,$opts:expr)=>{{

    let opts = $opts;
    let reverse = read(
        std::path::Path::new(&opts["--updates"])
            .parent()
            .unwrap()
            .join("reverse.json")
            .to_str()
            .unwrap(),
    );
    let engine = Arc::new(Mutex::new(Engine::new(
        $version,
        opts["--profile"].clone(),
        serde_json::from_value(read(&opts["--updates"])).unwrap(),
    )));
    if opts["--role"] == "agent" {
        Agent.$profile().on_receive_request({let engine=Arc::clone(&engine);let reverse=reverse.clone();async move |request:UntypedMessage,responder,connection|{
   if let Err(error)=validate_wire($version,request.method(),request.params()){return responder.respond_with_error(error);}
 if $version==2 && request.method()=="session/prompt" && request.params()["prompt"][0]["text"]=="request-cancel" {
 connection.send_notification(UntypedMessage::new("session/update",json!({"sessionId":request.params()["sessionId"],"update":{"sessionUpdate":"notice","severity":"info","title":"request-ready"}}))?)?;
 let cancellation=responder.cancellation();let engine=Arc::clone(&engine);let conn=connection.clone();let sid=request.params()["sessionId"].clone();
 return connection.spawn(async move{let result=cancellation.run_until_cancelled(std::future::pending::<Result<Value,Error>>()).await;engine.lock().unwrap().request_cancelled=true;conn.send_notification(UntypedMessage::new("session/update",json!({"sessionId":sid,"update":{"sessionUpdate":"notice","severity":"info","title":"request-cancelled"}}))?)?;responder.respond_with_result(result)});
 }
let result=engine.lock().unwrap().handle(request.method(),request.params());
   match result {Ok((r,updates))=>{let conn=connection.clone();let reverse=reverse.clone();connection.spawn(async move{for update in updates{conn.send_notification(UntypedMessage::new("session/update",json!({"sessionId":request.params()["sessionId"],"update":update}))?)?;if update["state"]=="requires_action"{for r in reverse.as_array().unwrap(){if r["notify"]==true{conn.send_notification(UntypedMessage::new(r["method"].as_str().unwrap(),r["params"].clone())?)?;continue;}let response=conn.send_request(UntypedMessage::new(r["method"].as_str().unwrap(),r["params"].clone())?).block_task().await?;matches(&r["response"],&response)?;}}}responder.respond(r)})},Err(e)=>responder.respond_with_error(e)}
  }},agent_client_protocol::on_receive_request!())
  .on_receive_notification({let engine=Arc::clone(&engine);async move |request:UntypedMessage,connection|{if request.method()=="session/cancel"{let(_,updates)=engine.lock().unwrap().handle(request.method(),request.params())?;for update in updates{connection.send_notification(UntypedMessage::new("session/update",json!({"sessionId":request.params()["sessionId"],"update":update}))?)?;}}Ok(())}},agent_client_protocol::on_receive_notification!()).connect_to(Stdio::new()).await?;
    } else {
        let case = read(&opts["--case"]);
        let updates = Arc::new(Mutex::new(Vec::<Value>::new()));
        let seen = Arc::clone(&updates);
        let reverse_trace = Arc::new(Mutex::new(Vec::<String>::new()));
        let reverse_seen = Arc::clone(&reverse_trace);
        let reverse_notifications = Arc::clone(&reverse_trace);
        let notification_vectors = reverse.clone();
        let trace = Client
            .$profile()
            .on_receive_notification(
                async move |n: UntypedMessage, _connection| {
                    if n.method() == "session/update" {
                        validate_wire($version, n.method(), n.params())?;
                        seen.lock().unwrap().push(n.params()["update"].clone());
                    } else if n.method() == "elicitation/complete" {
                        validate_wire($version, n.method(), n.params())?;
                        let v = notification_vectors
                            .as_array()
                            .unwrap()
                            .iter()
                            .find(|r| r["notify"] == true)
                            .unwrap();
                        matches(&v["params"], n.params())?;
                        reverse_notifications
                            .lock()
                            .unwrap()
                            .push("elicitation/complete".to_string());
                    }
                    Ok(())
                },
                agent_client_protocol::on_receive_notification!(),
            )
            .on_receive_request(
                async move |request: UntypedMessage, responder, _connection| {
                    validate_wire(
                        $version,
                        request.method(),
                        request.params(),
                    )?;
                    for r in reverse.as_array().unwrap() {
                        if r["method"] == request.method() {
                            if matches(&r["params"], request.params()).is_err() {
                                continue;
                            }
                            reverse_seen
                                .lock()
                                .unwrap()
                                .push(request.method().to_string());
                            return responder.respond(r["response"].clone());
                        }
                    }
                    responder.respond_with_error(Error::method_not_found())
                },
                agent_client_protocol::on_receive_request!(),
            )
            .connect_with(
                AcpAgent::new(AcpAgentConfig::new(&opts["--agent"]).args([
                    "-version",
                    &$version.to_string(),
                    "-profile",
                    &opts["--profile"],
                    "-updates",
                    &opts["--updates"],
                ])),
                async move |connection| {
                    let mut trace = vec![];
                    let mut offset = 0;
                    for (i, step) in case["steps"].as_array().unwrap().iter().enumerate() {
                        let method = step["method"].as_str().unwrap();
                        let expected = step["error"].as_i64();
                        if step["notify"] == true {
                            connection.send_notification(UntypedMessage::new(
                                method,
                                step["params"].clone(),
                            )?)?;
                        } else {
                            let pending = connection
                                .send_request(UntypedMessage::new(method, step["params"].clone())?);
                            if step["cancel"] == true {
                                let deadline = Instant::now() + Duration::from_secs(10);
                                while updates.lock().unwrap().len() <= offset {
                                    if Instant::now() > deadline {
                                        return Err(Error::internal_error()
                                            .data("missing cancel readiness"));
                                    }
                                    tokio::time::sleep(Duration::from_millis(2)).await;
                                }
                                pending.cancel()?;
                            }
                            let r = pending.block_task().await;
                            match (r, expected) {
                                (Err(e), Some(code)) if i64::from(i32::from(e.code)) == code => {}
                                (Ok(r), None) => matches(&step["expect"], &r)?,
                                (r, _) => {
                                    return Err(Error::internal_error()
                                        .data(format!("step {i} {method}: {r:?}")));
                                }
                            }
                        }
                        if let Some(wanted) = step["updates"].as_array() {
                            for (j, w) in wanted.iter().enumerate() {
                                let deadline = Instant::now() + Duration::from_secs(10);
                                loop {
                                    let actual = updates.lock().unwrap().get(offset + j).cloned();
                                    if let Some(actual) = actual {
                                        matches(w, &actual)?;
                                        break;
                                    }
                                    if Instant::now() > deadline {
                                        return Err(Error::internal_error()
                                            .data(format!("missing update {i}/{j}")));
                                    }
                                    tokio::time::sleep(Duration::from_millis(2)).await;
                                }
                            }
                            offset += wanted.len();
                        }
                        trace.push(format!("{i}:{method}:ok"));
                    }
                    Ok(trace)
                },
            )
            .await?;
        std::fs::write(
            &opts["--result"],
            serde_json::to_vec(&json!({"trace":trace,"reverse":*reverse_trace.lock().unwrap()}))?,
        )?;
    }

 }};
}
#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let mut opts = HashMap::new();
    let args = std::env::args().skip(1).collect::<Vec<_>>();
    for pair in args.chunks_exact(2) {
        opts.insert(pair[0].clone(), pair[1].clone());
    }
    match opts["--version"].as_str() {
        "1" => run_profile!(v1, 1u16, &opts),
        "2" => run_profile!(v2, 2u16, &opts),
        _ => return Err("invalid version".into()),
    };
    Ok(())
}

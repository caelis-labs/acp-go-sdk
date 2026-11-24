use agent_client_protocol::mcp_server::{
    McpOutcome, McpRejection, McpRequest, McpRequestContext, McpServer, McpService,
};
use agent_client_protocol::{Agent, Client, Error, NullRun, Stdio, UntypedMessage, mcp_admission};
use serde_json::json;
use std::future::{Future, pending};
use std::pin::Pin;
use std::sync::Arc;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};

#[derive(Default)]
struct Fixture(AtomicUsize);

impl McpService<Agent> for Fixture {
    fn execute(
        &self,
        _request: McpRequest,
        context: McpRequestContext<Agent>,
    ) -> Pin<Box<dyn Future<Output = Result<McpOutcome, Error>> + Send>> {
        let runs = self.0.fetch_add(1, Ordering::Relaxed) + 1;
        let result = json!({
            "sessionId": context.session_id(),
            "toolCallId": context.tool_call_id(),
            "runs": runs
        });
        Box::pin(async move { Ok(McpOutcome::Result(result)) })
    }
}

#[tokio::main]
async fn main() -> Result<(), Error> {
    let announced = Arc::new(AtomicBool::new(false));
    let admission = Arc::clone(&announced);
    let server = McpServer::new_service("attribution-fixture", Fixture::default(), NullRun)
        .with_admission(
            async move |request| {
                let session = request.require_session_id()?;
                if session.0.as_ref() != "child" || !admission.load(Ordering::Acquire) {
                    return Err(McpRejection::session_rejected(
                        "outside the fixture session",
                    ));
                }
                Ok(())
            },
            mcp_admission!(),
        );
    let ready = || UntypedMessage::new("_interop/mcp-ready", json!({}));
    macro_rules! observe_child {
        ($client:expr) => {
            $client.on_receive_notification(
                async move |message: UntypedMessage, conn| {
                    let p = message.params();
                    if message.method() == "session/update"
                        && p["sessionId"] == "parent"
                        && p["update"]["sessionUpdate"] == "subagent_update"
                        && p["update"]["sessionId"] == "child"
                    {
                        announced.store(true, Ordering::Release);
                        conn.send_notification(UntypedMessage::new(
                            "_interop/child-observed",
                            json!({}),
                        )?)?;
                    }
                    Ok(())
                },
                agent_client_protocol::on_receive_notification!(),
            )
        };
    }
    match std::env::args().nth(1).as_deref() {
        Some("1") => {
            use agent_client_protocol::{schema::ProtocolVersion, v1::schema};
            observe_child!(Client.v1())
                .connect_with(Stdio::new(), async move |cx| {
                    cx.send_request(
                        schema::InitializeRequest::new(ProtocolVersion::V1).client_capabilities(
                            schema::ClientCapabilities::new()
                                .subagents(schema::SubagentCapabilities::new()),
                        ),
                    )
                    .block_task()
                    .await?;
                    cx.build_session_cwd()?
                        .with_mcp_server(server)?
                        .block_task()
                        .run_until(async |_| {
                            cx.send_notification(ready()?)?;
                            pending().await
                        })
                        .await
                })
                .await
        }
        Some("2") => {
            use agent_client_protocol::{schema::ProtocolVersion, v2::schema};
            observe_child!(Client.v2())
                .connect_with(Stdio::new(), async move |cx| {
                    cx.send_request(schema::InitializeRequest::new(
                        ProtocolVersion::V2,
                        schema::Implementation::new("mcp-admission-fixture", "1"),
                    ))
                    .block_task()
                    .await?;
                    let _session = cx
                        .build_session_cwd()?
                        .with_mcp_server(server)?
                        .start_session()
                        .block_task()
                        .await?;
                    cx.send_notification(ready()?)?;
                    pending().await
                })
                .await
        }
        _ => Err(Error::invalid_params().data("expected ACP profile 1 or 2")),
    }
}

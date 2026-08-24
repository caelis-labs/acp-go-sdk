use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use agent_client_protocol::schema::ProtocolVersion;
use agent_client_protocol::schema::v1::{
    AgentCapabilities, CancelNotification, ContentBlock, ContentChunk, InitializeRequest,
    InitializeResponse, NewSessionRequest, NewSessionResponse, PermissionOption,
    PermissionOptionKind, PromptRequest, PromptResponse, RequestPermissionOutcome,
    RequestPermissionRequest, RequestPermissionResponse, SelectedPermissionOutcome, SessionId,
    SessionNotification, SessionUpdate, StopReason, TextContent, ToolCallStatus, ToolCallUpdate,
    ToolCallUpdateFields, ToolKind,
};
use agent_client_protocol::{
    AcpAgent, AcpAgentConfig, Agent, Client, ConnectionTo, Responder, Stdio,
};
use serde::Serialize;
use tokio::sync::oneshot;

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "kebab-case")]
enum Scenario {
    Core,
    SessionCancel,
    RequestCancel,
}

impl Scenario {
    fn parse(value: &str) -> Result<Self, String> {
        match value {
            "core" => Ok(Self::Core),
            "session-cancel" => Ok(Self::SessionCancel),
            "request-cancel" => Ok(Self::RequestCancel),
            _ => Err(format!("unsupported scenario {value:?}")),
        }
    }

    const fn as_str(self) -> &'static str {
        match self {
            Self::Core => "core",
            Self::SessionCancel => "session-cancel",
            Self::RequestCancel => "request-cancel",
        }
    }
}

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "lowercase")]
enum Role {
    Agent,
    Client,
}

impl Role {
    fn parse(value: &str) -> Result<Self, String> {
        match value {
            "agent" => Ok(Self::Agent),
            "client" => Ok(Self::Client),
            _ => Err(format!("unsupported role {value:?}")),
        }
    }
}

#[derive(Debug)]
struct Options {
    role: Role,
    scenario: Scenario,
    result_path: Option<PathBuf>,
    agent_path: Option<PathBuf>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
struct Observation {
    peer: &'static str,
    role: Role,
    scenario: Scenario,
    #[serde(skip_serializing_if = "Option::is_none")]
    protocol_version: Option<u16>,
    #[serde(skip_serializing_if = "Option::is_none")]
    capabilities_unsupported: Option<bool>,
    #[serde(skip_serializing_if = "Option::is_none")]
    session_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    stop_reason: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error_code: Option<i32>,
    #[serde(skip_serializing_if = "Option::is_none")]
    request_cancel_observed: Option<bool>,
    events: Vec<String>,
}

fn parse_options() -> Result<Options, String> {
    let mut values = HashMap::new();
    let mut args = std::env::args().skip(1);
    while let Some(key) = args.next() {
        let Some(value) = args.next() else {
            return Err(format!("missing value for {key}"));
        };
        let Some(name) = key.strip_prefix("--") else {
            return Err(format!("unexpected argument {key:?}"));
        };
        values.insert(name.to_string(), value);
    }

    Ok(Options {
        role: Role::parse(
            values
                .get("role")
                .ok_or_else(|| "--role is required".to_string())?,
        )?,
        scenario: Scenario::parse(
            values
                .get("scenario")
                .ok_or_else(|| "--scenario is required".to_string())?,
        )?,
        result_path: values.get("result").map(PathBuf::from),
        agent_path: values.get("agent").map(PathBuf::from),
    })
}

fn write_observation(path: &Path, observation: &Observation) -> Result<(), std::io::Error> {
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)?;
    }
    let temporary = path.with_extension(format!("tmp-{}", std::process::id()));
    let mut encoded = serde_json::to_vec(observation)?;
    encoded.push(b'\n');
    fs::write(&temporary, encoded)?;
    fs::rename(temporary, path)
}

fn prompt_text(prompt: &[ContentBlock]) -> String {
    prompt
        .iter()
        .filter_map(|block| match block {
            ContentBlock::Text(text) => Some(text.text.as_str()),
            _ => None,
        })
        .collect::<Vec<_>>()
        .join(" ")
}

fn update_text(notification: &SessionNotification) -> Option<&str> {
    let SessionUpdate::AgentMessageChunk(chunk) = &notification.update else {
        return None;
    };
    let ContentBlock::Text(text) = &chunk.content else {
        return None;
    };
    Some(text.text.as_str())
}

fn stop_reason_name(reason: &StopReason) -> String {
    serde_json::to_value(reason)
        .ok()
        .and_then(|value| value.as_str().map(str::to_owned))
        .unwrap_or_else(|| format!("{reason:?}"))
}

fn protocol_version_number(version: &ProtocolVersion) -> u16 {
    serde_json::to_value(version)
        .ok()
        .and_then(|value| value.as_u64())
        .and_then(|value| u16::try_from(value).ok())
        .unwrap_or_default()
}

fn send_update(
    connection: &ConnectionTo<Client>,
    session_id: &SessionId,
    text: &str,
) -> Result<(), agent_client_protocol::Error> {
    connection.send_notification(SessionNotification::new(
        session_id.clone(),
        SessionUpdate::AgentMessageChunk(ContentChunk::new(ContentBlock::Text(TextContent::new(
            text,
        )))),
    ))
}

async fn wait_for_event(
    events: &Arc<Mutex<Vec<String>>>,
    expected: &str,
) -> Result<(), agent_client_protocol::Error> {
    let deadline = Instant::now() + Duration::from_secs(10);
    loop {
        if events
            .lock()
            .expect("events mutex poisoned")
            .iter()
            .any(|event| event == expected)
        {
            return Ok(());
        }
        if Instant::now() >= deadline {
            return Err(agent_client_protocol::Error::internal_error()
                .data(format!("timed out waiting for {expected}")));
        }
        tokio::time::sleep(Duration::from_millis(10)).await;
    }
}

async fn run_agent(options: Options) -> Result<(), agent_client_protocol::Error> {
    let next_session = Arc::new(AtomicU64::new(0));
    let session_cancels = Arc::new(Mutex::new(HashMap::<String, oneshot::Sender<()>>::new()));
    let result_path = options.result_path.clone();

    Agent
        .builder()
        .name("acp-go-sdk-rust-interop-agent")
        .on_receive_request(
            async move |request: InitializeRequest,
                        responder: Responder<InitializeResponse>,
                        _connection| {
                responder.respond(
                    InitializeResponse::new(request.protocol_version)
                        .agent_capabilities(AgentCapabilities::new()),
                )
            },
            agent_client_protocol::on_receive_request!(),
        )
        .on_receive_request(
            {
                let next_session = Arc::clone(&next_session);
                async move |_request: NewSessionRequest,
                            responder: Responder<NewSessionResponse>,
                            _connection| {
                    let id = next_session.fetch_add(1, Ordering::Relaxed) + 1;
                    responder.respond(NewSessionResponse::new(SessionId::new(format!(
                        "rust-session-{id}"
                    ))))
                }
            },
            agent_client_protocol::on_receive_request!(),
        )
        .on_receive_request(
            {
                let session_cancels = Arc::clone(&session_cancels);
                async move |request: PromptRequest,
                            responder: Responder<PromptResponse>,
                            connection: ConnectionTo<Client>| {
                    let scenario = prompt_text(&request.prompt);
                    let session_id = request.session_id.clone();

                    match scenario.as_str() {
                        "core" => connection.spawn({
                            let connection = connection.clone();
                            async move {
                                send_update(&connection, &session_id, "core-1")?;
                                send_update(&connection, &session_id, "core-2")?;
                                let permission = RequestPermissionRequest::new(
                                    session_id.clone(),
                                    ToolCallUpdate::new(
                                        "interop-tool",
                                        ToolCallUpdateFields::new()
                                            .title("Interop permission")
                                            .kind(ToolKind::Execute)
                                            .status(ToolCallStatus::Pending),
                                    ),
                                    vec![
                                        PermissionOption::new(
                                            "allow",
                                            "Allow",
                                            PermissionOptionKind::AllowOnce,
                                        ),
                                        PermissionOption::new(
                                            "reject",
                                            "Reject",
                                            PermissionOptionKind::RejectOnce,
                                        ),
                                    ],
                                );
                                let response =
                                    connection.send_request(permission).block_task().await?;
                                match response.outcome {
                                    RequestPermissionOutcome::Selected(selected)
                                        if selected.option_id.to_string() == "allow" => {}
                                    outcome => {
                                        return Err(agent_client_protocol::Error::internal_error()
                                            .data(format!(
                                                "unexpected permission outcome: {outcome:?}"
                                            )));
                                    }
                                }
                                send_update(&connection, &session_id, "core-3")?;
                                responder.respond(PromptResponse::new(StopReason::EndTurn))
                            }
                        }),
                        "session-cancel" => {
                            let (cancel_tx, cancel_rx) = oneshot::channel();
                            session_cancels
                                .lock()
                                .expect("session cancel mutex poisoned")
                                .insert(session_id.to_string(), cancel_tx);
                            send_update(&connection, &session_id, "session-cancel-ready")?;
                            connection.spawn(async move {
                                let _ = cancel_rx.await;
                                responder.respond(PromptResponse::new(StopReason::Cancelled))
                            })
                        }
                        "request-cancel" => {
                            send_update(&connection, &session_id, "request-cancel-ready")?;
                            let cancellation = responder.cancellation();
                            let result_path = result_path.clone();
                            connection.spawn(async move {
                                let response = cancellation
                                    .run_until_cancelled(std::future::pending::<
                                        Result<PromptResponse, agent_client_protocol::Error>,
                                    >())
                                    .await;
                                if let Some(path) = result_path {
                                    write_observation(
                                        &path,
                                        &Observation {
                                            peer: "rust",
                                            role: Role::Agent,
                                            scenario: Scenario::RequestCancel,
                                            protocol_version: None,
                                            capabilities_unsupported: None,
                                            session_id: None,
                                            stop_reason: None,
                                            error_code: None,
                                            request_cancel_observed: Some(true),
                                            events: vec!["request-cancel-observed".to_string()],
                                        },
                                    )
                                    .map_err(|error| {
                                        agent_client_protocol::Error::internal_error()
                                            .data(error.to_string())
                                    })?;
                                }
                                responder.respond_with_result(response)
                            })
                        }
                        _ => responder.respond_with_error(
                            agent_client_protocol::Error::invalid_params().data(scenario),
                        ),
                    }
                }
            },
            agent_client_protocol::on_receive_request!(),
        )
        .on_receive_notification(
            {
                let session_cancels = Arc::clone(&session_cancels);
                async move |notification: CancelNotification, _connection| {
                    if let Some(cancel) = session_cancels
                        .lock()
                        .expect("session cancel mutex poisoned")
                        .remove(&notification.session_id.to_string())
                    {
                        let _ = cancel.send(());
                    }
                    Ok(())
                }
            },
            agent_client_protocol::on_receive_notification!(),
        )
        .connect_to(Stdio::new())
        .await
}

async fn run_client(options: Options) -> Result<(), agent_client_protocol::Error> {
    let agent_path = options.agent_path.as_ref().ok_or_else(|| {
        agent_client_protocol::Error::invalid_params().data("--agent is required")
    })?;
    let result_path = options.result_path.as_ref().ok_or_else(|| {
        agent_client_protocol::Error::invalid_params().data("--result is required")
    })?;
    let events = Arc::new(Mutex::new(Vec::<String>::new()));
    let observed_events = Arc::clone(&events);
    let scenario = options.scenario;

    let (protocol_version, capabilities_unsupported, session_id, stop_reason, error_code) = Client
        .builder()
        .on_receive_notification(
            {
                let events = Arc::clone(&events);
                async move |notification: SessionNotification, _connection| {
                    if let Some(text) = update_text(&notification) {
                        events
                            .lock()
                            .expect("events mutex poisoned")
                            .push(format!("update:{text}"));
                    }
                    Ok(())
                }
            },
            agent_client_protocol::on_receive_notification!(),
        )
        .on_receive_request(
            {
                let events = Arc::clone(&events);
                async move |request: RequestPermissionRequest,
                            responder: Responder<RequestPermissionResponse>,
                            _connection| {
                    let option = request
                        .options
                        .iter()
                        .find(|option| option.option_id.to_string() == "allow")
                        .ok_or_else(|| {
                            agent_client_protocol::Error::invalid_params()
                                .data("Go agent did not offer allow")
                        })?;
                    events
                        .lock()
                        .expect("events mutex poisoned")
                        .push("permission:allow".to_string());
                    responder.respond(RequestPermissionResponse::new(
                        RequestPermissionOutcome::Selected(SelectedPermissionOutcome::new(
                            option.option_id.clone(),
                        )),
                    ))
                }
            },
            agent_client_protocol::on_receive_request!(),
        )
        .connect_with(AcpAgent::new(AcpAgentConfig::new(agent_path)), {
            let events = Arc::clone(&events);
            async move |connection: ConnectionTo<Agent>| {
                let initialized = connection
                    .send_request(InitializeRequest::new(ProtocolVersion::V1))
                    .block_task()
                    .await?;
                let capabilities_unsupported = !initialized.agent_capabilities.load_session
                    && !initialized.agent_capabilities.prompt_capabilities.audio
                    && !initialized
                        .agent_capabilities
                        .prompt_capabilities
                        .embedded_context
                    && !initialized.agent_capabilities.prompt_capabilities.image;
                let session = connection
                    .send_request(NewSessionRequest::new(std::env::current_dir().map_err(
                        |error| {
                            agent_client_protocol::Error::internal_error().data(error.to_string())
                        },
                    )?))
                    .block_task()
                    .await?;
                let request = PromptRequest::new(
                    session.session_id.clone(),
                    vec![ContentBlock::Text(TextContent::new(scenario.as_str()))],
                );

                match scenario {
                    Scenario::Core => {
                        let response = connection.send_request(request).block_task().await?;
                        let stop_reason = stop_reason_name(&response.stop_reason);
                        events
                            .lock()
                            .expect("events mutex poisoned")
                            .push(format!("response:{stop_reason}"));
                        Ok((
                            initialized.protocol_version,
                            capabilities_unsupported,
                            session.session_id,
                            Some(stop_reason),
                            None,
                        ))
                    }
                    Scenario::SessionCancel => {
                        let response = connection.send_request(request);
                        wait_for_event(&events, "update:session-cancel-ready").await?;
                        connection.send_notification(CancelNotification::new(
                            session.session_id.clone(),
                        ))?;
                        let response = response.block_task().await?;
                        let stop_reason = stop_reason_name(&response.stop_reason);
                        events
                            .lock()
                            .expect("events mutex poisoned")
                            .push(format!("response:{stop_reason}"));
                        Ok((
                            initialized.protocol_version,
                            capabilities_unsupported,
                            session.session_id,
                            Some(stop_reason),
                            None,
                        ))
                    }
                    Scenario::RequestCancel => {
                        let response = connection.send_request(request);
                        wait_for_event(&events, "update:request-cancel-ready").await?;
                        response.cancel()?;
                        let error = response
                            .block_task()
                            .await
                            .expect_err("request cancellation must return an error");
                        let code = i32::from(error.code);
                        events
                            .lock()
                            .expect("events mutex poisoned")
                            .push(format!("error:{code}"));
                        Ok((
                            initialized.protocol_version,
                            capabilities_unsupported,
                            session.session_id,
                            None,
                            Some(code),
                        ))
                    }
                }
            }
        })
        .await?;

    write_observation(
        result_path,
        &Observation {
            peer: "rust",
            role: Role::Client,
            scenario,
            protocol_version: Some(protocol_version_number(&protocol_version)),
            capabilities_unsupported: Some(capabilities_unsupported),
            session_id: Some(session_id.to_string()),
            stop_reason,
            error_code,
            request_cancel_observed: None,
            events: observed_events
                .lock()
                .expect("events mutex poisoned")
                .clone(),
        },
    )
    .map_err(|error| agent_client_protocol::Error::internal_error().data(error.to_string()))
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let options = parse_options().map_err(std::io::Error::other)?;
    match options.role {
        Role::Agent => run_agent(options).await?,
        Role::Client => run_client(options).await?,
    }
    Ok(())
}

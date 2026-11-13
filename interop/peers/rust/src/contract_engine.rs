use agent_client_protocol::Error;
use serde_json::{Value, json};
use std::collections::BTreeMap;
pub struct Engine {
    version: u16,
    profile: String,
    sessions: BTreeMap<String, Value>,
    mutations: u64,
    foreground: bool,
    background: bool,
    pub request_cancelled: bool,
    tool_cancelable: bool,
    terminal_cancelable: bool,
    pending: Vec<Value>,
    history: BTreeMap<String, Vec<Value>>,
    logged_in: bool,
    config: bool,
    vectors: Vec<Value>,
}
fn invalid() -> Error {
    Error::invalid_params().data("contract fixture rejected request")
}
impl Engine {
    pub fn new(version: u16, profile: String, vectors: Vec<Value>) -> Self {
        Self {
            version,
            profile,
            sessions: BTreeMap::new(),
            mutations: 0,
            foreground: false,
            background: false,
            request_cancelled: false,
            tool_cancelable: false,
            terminal_cancelable: false,
            pending: vec![],
            history: BTreeMap::new(),
            logged_in: false,
            config: false,
            vectors,
        }
    }
    fn missing(&self) -> Error {
        if self.version == 2 {
            Error::new(-32002, "Session not found")
        } else {
            invalid()
        }
    }
    pub fn handle(&mut self, method: &str, p: &Value) -> Result<(Value, Vec<Value>), Error> {
        let done = |v| Ok((v, vec![]));
        if ["no-session", "null-session"].contains(&self.profile.as_str())
            && method.starts_with("session/")
        {
            return Err(invalid());
        }
        match method {
            "initialize" => {
                let mut caps = json!({});
                match self.profile.as_str() {
                    "full" => caps["systemPrompt"] = json!({"mode":{"override":{}}}),
                    "append" => caps["systemPrompt"] = json!({}),
                    "null" => caps["systemPrompt"] = Value::Null,
                    _ => {}
                };
                if self.version == 2 {
                    if ["no-session", "null-session"].contains(&self.profile.as_str()) {
                        return done(
                            json!({"protocolVersion":2,"info":{"name":"rust-contract","version":"1"},"capabilities":if self.profile=="no-session"{json!({})}else{json!({"session":null})}}),
                        );
                    };
                    caps["delete"] = json!({});
                    caps["mcp"] = json!({"stdio":{}});
                    done(
                        json!({"protocolVersion":2,"info":{"name":"rust-contract","version":"1"},"capabilities":{"session":caps},"authMethods":[{"type":"agent","methodId":"fixture","name":"Fixture"}]}),
                    )
                } else {
                    caps["list"] = json!({});
                    caps["resume"] = json!({});
                    done(
                        json!({"protocolVersion":1,"agentCapabilities":{"loadSession":true,"sessionCapabilities":caps}}),
                    )
                }
            }
            "_interop/flush" => Ok((json!({}), std::mem::take(&mut self.pending))),
            "auth/login" => {
                if p["methodId"] != "fixture" {
                    return Err(invalid());
                };
                self.logged_in = true;
                done(json!({}))
            }
            "auth/logout" => {
                self.logged_in = false;
                done(json!({}))
            }
            "session/set_config_option" => {
                if p["configId"] != "verbose" || p["type"] != "boolean" || !p["value"].is_boolean()
                {
                    return Err(invalid());
                };
                let id = p["sessionId"].as_str().ok_or_else(invalid)?;
                if !self.sessions.contains_key(id) {
                    return Err(self.missing());
                };
                self.config = p["value"].as_bool().unwrap();
                self.mutations += 1;
                done(
                    json!({"configOptions":[{"configId":"verbose","name":"Verbose","type":"boolean","currentValue":self.config}]}),
                )
            }
            "_interop/inspect" => done(
                json!({"mutations":self.mutations,"sessions":self.sessions,"foreground":self.foreground,"background":self.background,"requestCancelled":self.request_cancelled,"loggedIn":self.logged_in,"config":self.config}),
            ),
            "session/new" | "session/load" | "session/resume" => {
                if self.version == 2 && method == "session/load" {
                    return Err(Error::method_not_found());
                }
                if !p["replayFrom"].is_null() && p["replayFrom"]["type"] != "start" {
                    return Err(invalid());
                };
                let mut prompt = String::new();
                let sp = &p["systemPrompt"];
                if !sp.is_null() {
                    let mode = sp["mode"].as_str().ok_or_else(invalid)?;
                    let content = sp["content"].as_array().ok_or_else(invalid)?;
                    if !["full", "append"].contains(&self.profile.as_str())
                        || !["append", "override"].contains(&mode)
                        || (mode == "override" && self.profile != "full")
                        || content.is_empty()
                    {
                        return Err(invalid());
                    };
                    let mut texts = vec![];
                    for block in content {
                        if block["type"] != "text" {
                            return Err(invalid());
                        };
                        texts.push(block["text"].as_str().ok_or_else(invalid)?)
                    }
                    prompt = format!("{}:{}", mode, texts.join(" "));
                }
                if p["cwd"].as_str().is_none_or(str::is_empty) {
                    return Err(invalid());
                }
                let id = if method == "session/new" {
                    format!("s{}", self.sessions.len() + 1)
                } else {
                    p["sessionId"].as_str().ok_or_else(invalid)?.to_string()
                };
                if method == "session/new" {
                    self.sessions.insert(id.clone(),json!({"prompt":"default","permissions":"ask","mcpServers":p.get("mcpServers").cloned().unwrap_or(json!([])),"active":true}));
                }
                let missing = self.missing();
                let s = self.sessions.get_mut(&id).ok_or(missing)?;
                if !prompt.is_empty() {
                    s["prompt"] = json!(prompt)
                };
                s["active"] = json!(true);
                self.mutations += 1;
                if method == "session/new" {
                    {
                        let mut result = json!({"sessionId":id});
                        if self.version == 2 {
                            result["configOptions"] = json!([{"configId":"verbose","name":"Verbose","type":"boolean","currentValue":self.config}]);
                        }
                        done(result)
                    }
                } else if self.version == 2 {
                    {
                        let mut updates = if p["replayFrom"]["type"] == "start" {
                            self.history.get(&id).cloned().unwrap_or_default()
                        } else {
                            vec![]
                        };
                        updates.push(json!({"sessionUpdate":"state_update","state":"idle","stopReason":"end_turn","background":[]}));
                        Ok((json!({}), updates))
                    }
                } else {
                    done(json!({}))
                }
            }
            "session/list" => done(
                json!({"sessions":self.sessions.keys().map(|id|json!({"sessionId":id,"cwd":"/tmp"})).collect::<Vec<_>>()}),
            ),
            "session/close" | "session/delete" => {
                let id = p["sessionId"].as_str().ok_or_else(invalid)?;
                if !self.sessions.contains_key(id) {
                    return Err(self.missing());
                };
                if method == "session/delete" {
                    self.sessions.remove(id);
                } else {
                    self.sessions.get_mut(id).unwrap()["active"] = json!(false);
                    self.foreground = false;
                    self.background = false
                };
                self.mutations += 1;
                done(json!({}))
            }
            "session/prompt" => {
                let id = p["sessionId"].as_str().ok_or_else(invalid)?;
                if self.sessions.get(id).is_none_or(|s| s["active"] != true) {
                    return Err(self.missing());
                };
                let blocks = p["prompt"].as_array().ok_or_else(invalid)?;
                if blocks.is_empty() {
                    return Err(invalid());
                };
                for b in blocks {
                    if b["type"] != "text" && b["type"] != "resource_link" {
                        return Err(invalid());
                    }
                }
                self.history.entry(id.to_string()).or_default().push(json!({"sessionUpdate":"user_message","messageId":if blocks[0]["text"]=="cancel"{"m2"}else{"m1"},"content":blocks}));
                let mut outgoing = self.vectors.clone();
                outgoing[0]["content"] = json!(blocks);
                self.tool_cancelable = true;
                self.terminal_cancelable = true;
                if blocks[0]["text"] == "cancel" {
                    self.foreground = true;
                    self.background = true;
                    Ok((
                        json!({"messageId":"m2"}),
                        vec![
                            json!({"sessionUpdate":"user_message","messageId":"m2","content":blocks}),
                            json!({"sessionUpdate":"state_update","state":"running","background":[{"type":"tool_call","toolCallId":"bg"}]}),
                        ],
                    ))
                } else if blocks[0]["text"] == "after" {
                    self.pending = outgoing;
                    self.pending[0]["content"] = json!(blocks);
                    done(json!({"messageId":"m1"}))
                } else {
                    Ok((json!({"messageId":"m1"}), outgoing))
                }
            }
            "session/cancel" => {
                self.foreground = false;
                Ok((
                    json!({}),
                    vec![
                        json!({"sessionUpdate":"state_update","state":"idle","stopReason":"cancelled","background":[{"type":"tool_call","toolCallId":"bg"}]}),
                    ],
                ))
            }
            "tool_call/cancel" => {
                if p["toolCallId"] != "t2" || !self.tool_cancelable {
                    return Err(invalid());
                };
                self.tool_cancelable = false;
                Ok((
                    json!({}),
                    vec![
                        json!({"sessionUpdate":"tool_call_update","toolCallId":"t2","status":"completed","capabilities":null}),
                    ],
                ))
            }
            "terminal/cancel" => {
                if p["terminalId"] != "term2" || !self.terminal_cancelable {
                    return Err(invalid());
                };
                self.terminal_cancelable = false;
                Ok((
                    json!({}),
                    vec![
                        json!({"sessionUpdate":"terminal_update","terminalId":"term2","exitStatus":{"exitCode":130},"capabilities":null}),
                    ],
                ))
            }
            _ => Err(Error::method_not_found()),
        }
    }
}
pub fn matches(w: &Value, g: &Value) -> Result<(), Error> {
    match w {
        Value::Object(m) => {
            if !g.is_object() {
                return Err(invalid());
            }
            for (k, v) in m {
                matches(
                    v,
                    g.get(k)
                        .ok_or_else(|| Error::internal_error().data(format!("missing {k}")))?,
                )?
            }
        }
        Value::Array(a) => {
            let b = g.as_array().ok_or_else(invalid)?;
            if b.len() != a.len() {
                return Err(invalid());
            };
            for (i, v) in a.iter().enumerate() {
                matches(v, &b[i])?
            }
        }
        _ => {
            if w != g {
                return Err(Error::internal_error().data(format!("expected {w} got {g}")));
            }
        }
    };
    Ok(())
}

// Decode known messages with the selected official schema before fixture policy.
// Extension methods remain opaque. The SDK owns envelopes and protocol guards.
pub fn validate_wire(version: u16, method: &str, params: &Value) -> Result<(), Error> {
    macro_rules! decode {
        ($ty:ty) => {
            serde_json::from_value::<$ty>(params.clone())
                .map(|_| ())
                .map_err(|e| Error::invalid_params().data(e.to_string()))
        };
    }
    if version == 2 {
        use agent_client_protocol::v2::schema as s;
        match method {
            "initialize" => decode!(s::InitializeRequest),
            "session/new" => decode!(s::NewSessionRequest),
            "session/resume" => decode!(s::ResumeSessionRequest),
            "session/prompt" => decode!(s::PromptRequest),
            "session/update" => decode!(s::UpdateSessionNotification),
            "session/list" => decode!(s::ListSessionsRequest),
            "session/close" => decode!(s::CloseSessionRequest),
            "session/delete" => decode!(s::DeleteSessionRequest),
            "auth/login" => decode!(s::LoginAuthRequest),
            "auth/logout" => decode!(s::LogoutAuthRequest),
            "session/set_config_option" => decode!(s::SetSessionConfigOptionRequest),
            "tool_call/cancel" => decode!(s::CancelToolCallRequest),
            "terminal/cancel" => decode!(s::CancelTerminalRequest),
            "session/cancel" => decode!(s::CancelSessionNotification),
            "session/request_permission" => decode!(s::RequestPermissionRequest),
            "elicitation/create" => decode!(s::CreateElicitationRequest),
            "elicitation/complete" => decode!(s::CompleteElicitationNotification),
            _ => Ok(()),
        }
    } else {
        use agent_client_protocol::v1::schema as s;
        match method {
            "initialize" => decode!(s::InitializeRequest),
            "session/new" => decode!(s::NewSessionRequest),
            "session/load" => decode!(s::LoadSessionRequest),
            "session/resume" => decode!(s::ResumeSessionRequest),
            _ => Ok(()),
        }
    }
}

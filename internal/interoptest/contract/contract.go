// Package contract provides test-only application behavior for real SDK peers.
// It is not an ACP runtime and never executes its fixture MCP/tool commands.
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	acp "github.com/caelis-labs/acp-go-sdk"
	v2 "github.com/caelis-labs/acp-go-sdk/experimental/v2"
)

type Object = map[string]any

func Read(path string, out any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func object(v any) Object        { m, _ := v.(map[string]any); return m }
func invalid() *acp.RequestError { return acp.NewInvalidParams("contract fixture rejected request") }

type Session struct {
	Prompt      string `json:"prompt"`
	Permissions string `json:"permissions"`
	McpServers  any    `json:"mcpServers"`
	Active      bool   `json:"active"`
}
type Engine struct {
	sync.Mutex
	Version            int
	Profile            string
	Sessions           map[string]*Session
	Mutations          int
	Foreground         bool
	Background         bool
	RequestCancelled   bool
	ToolCancelable     bool
	TerminalCancelable bool
	Pending            []Object
	History            map[string][]Object
	LoggedIn           bool
	Config             bool
	updates            []Object
}

func New(version int, profile, updatesPath string) (*Engine, error) {
	e := &Engine{Version: version, Profile: profile, Sessions: map[string]*Session{}, History: map[string][]Object{}}
	err := Read(updatesPath, &e.updates)
	return e, err
}
func (e *Engine) Handle(method string, p Object) (any, []Object, *acp.RequestError) {
	e.Lock()
	defer e.Unlock()
	if (e.Profile == "no-session" || e.Profile == "null-session") && strings.HasPrefix(method, "session/") {
		return nil, nil, invalid()
	}
	switch method {
	case "initialize":
		caps := Object{}
		switch e.Profile {
		case "null":
			caps["systemPrompt"] = nil
		case "append":
			caps["systemPrompt"] = Object{}
		case "full":
			caps["systemPrompt"] = Object{"mode": Object{"override": Object{}}}
		}
		if e.Version == 2 {
			if e.Profile == "no-session" {
				return Object{"protocolVersion": 2, "info": Object{"name": "go-contract", "version": "1"}, "capabilities": Object{}}, nil, nil
			}
			if e.Profile == "null-session" {
				return Object{"protocolVersion": 2, "info": Object{"name": "go-contract", "version": "1"}, "capabilities": Object{"session": nil}}, nil, nil
			}
			caps["delete"] = Object{}
			caps["mcp"] = Object{"stdio": Object{}}
			return Object{"protocolVersion": 2, "info": Object{"name": "go-contract", "version": "1"}, "capabilities": Object{"session": caps}, "authMethods": []any{Object{"type": "agent", "methodId": "fixture", "name": "Fixture"}}}, nil, nil
		}
		caps["list"] = Object{}
		caps["resume"] = Object{}
		return Object{"protocolVersion": 1, "agentCapabilities": Object{"loadSession": true, "sessionCapabilities": caps}}, nil, nil
	case "_interop/flush":
		updates := e.Pending
		e.Pending = nil
		return Object{}, updates, nil
	case "auth/login":
		if p["methodId"] != "fixture" {
			return nil, nil, invalid()
		}
		e.LoggedIn = true
		return Object{}, nil, nil
	case "auth/logout":
		e.LoggedIn = false
		return Object{}, nil, nil
	case "session/set_config_option":
		value, ok := p["value"].(bool)
		if p["configId"] != "verbose" || p["type"] != "boolean" || !ok {
			return nil, nil, invalid()
		}
		id, _ := p["sessionId"].(string)
		if e.Sessions[id] == nil {
			return nil, nil, e.missing()
		}
		e.Config = value
		e.Mutations++
		return Object{"configOptions": []any{Object{"configId": "verbose", "name": "Verbose", "type": "boolean", "currentValue": value}}}, nil, nil
	case "_interop/inspect":
		snapshot := map[string]Session{}
		for id, s := range e.Sessions {
			snapshot[id] = *s
		}
		return Object{"mutations": e.Mutations, "sessions": snapshot, "foreground": e.Foreground, "background": e.Background, "requestCancelled": e.RequestCancelled, "loggedIn": e.LoggedIn, "config": e.Config}, nil, nil
	case "session/new", "session/load", "session/resume":
		if e.Version == 2 && method == "session/load" {
			return nil, nil, acp.NewMethodNotFound(method)
		}
		if cursor := object(p["replayFrom"]); cursor != nil && cursor["type"] != "start" {
			return nil, nil, invalid()
		}
		prompt := ""
		if sp := p["systemPrompt"]; sp != nil {
			s := object(sp)
			mode, _ := s["mode"].(string)
			content, _ := s["content"].([]any)
			if (e.Profile != "full" && e.Profile != "append") || (mode != "append" && mode != "override") || (mode == "override" && e.Profile != "full") || len(content) == 0 {
				return nil, nil, invalid()
			}
			prompt = mode + ":"
			for i, b := range content {
				block := object(b)
				text, ok := block["text"].(string)
				if block["type"] != "text" || !ok {
					return nil, nil, invalid()
				}
				if i > 0 {
					prompt += " "
				}
				prompt += text
			}
		}
		if cwd, ok := p["cwd"].(string); !ok || cwd == "" {
			return nil, nil, invalid()
		}
		id, _ := p["sessionId"].(string)
		if method == "session/new" {
			id = fmt.Sprintf("s%d", len(e.Sessions)+1)
			mcp := p["mcpServers"]
			if mcp == nil {
				mcp = []any{}
			}
			e.Sessions[id] = &Session{Prompt: "default", Permissions: "ask", McpServers: mcp, Active: true}
		} else if e.Sessions[id] == nil {
			return nil, nil, e.missing()
		}
		session := e.Sessions[id]
		if prompt != "" {
			session.Prompt = prompt
		}
		session.Active = true
		e.Mutations++
		if method == "session/new" {
			r := Object{"sessionId": id}
			if e.Version == 2 {
				r["configOptions"] = []any{Object{"configId": "verbose", "name": "Verbose", "type": "boolean", "currentValue": e.Config}}
			}
			return r, nil, nil
		}
		if e.Version == 2 {
			updates := []Object{}
			if cursor := object(p["replayFrom"]); cursor["type"] == "start" {
				updates = append(updates, e.History[id]...)
			}
			updates = append(updates, Object{"sessionUpdate": "state_update", "state": "idle", "stopReason": "end_turn", "background": []any{}})
			return Object{}, updates, nil
		}
		return Object{}, nil, nil
	case "session/list":
		ids := []string{}
		for id := range e.Sessions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		sessions := []any{}
		for _, id := range ids {
			sessions = append(sessions, Object{"sessionId": id, "cwd": "/tmp"})
		}
		return Object{"sessions": sessions}, nil, nil
	case "session/close", "session/delete":
		id, _ := p["sessionId"].(string)
		s := e.Sessions[id]
		if s == nil {
			return nil, nil, e.missing()
		}
		if method == "session/delete" {
			delete(e.Sessions, id)
		} else {
			s.Active = false
			e.Foreground = false
			e.Background = false
		}
		e.Mutations++
		return Object{}, nil, nil
	case "session/prompt":
		id, _ := p["sessionId"].(string)
		s := e.Sessions[id]
		if s == nil || !s.Active {
			return nil, nil, e.missing()
		}
		blocks, _ := p["prompt"].([]any)
		if len(blocks) == 0 {
			return nil, nil, invalid()
		}
		for _, b := range blocks {
			block := object(b)
			if block["type"] != "text" && block["type"] != "resource_link" {
				return nil, nil, invalid()
			}
		}
		text, _ := object(blocks[0])["text"].(string)
		messageID := "m1"
		if text == "cancel" {
			messageID = "m2"
		}
		e.History[id] = append(e.History[id], Object{"sessionUpdate": "user_message", "messageId": messageID, "content": blocks})
		vectors := append([]Object(nil), e.updates...)
		vectors[0] = Object{"sessionUpdate": "user_message", "messageId": "m1", "content": blocks}
		if text == "cancel" {
			e.Foreground = true
			e.Background = true
			return Object{"messageId": "m2"}, []Object{{"sessionUpdate": "user_message", "messageId": "m2", "content": blocks}, {"sessionUpdate": "state_update", "state": "running", "background": []any{Object{"type": "tool_call", "toolCallId": "bg"}}}}, nil
		}
		e.ToolCancelable = true
		e.TerminalCancelable = true
		if text == "after" {
			e.Pending = vectors
			e.Pending[0] = Object{"sessionUpdate": "user_message", "messageId": "m1", "content": blocks}
			return Object{"messageId": "m1"}, nil, nil
		}
		return Object{"messageId": "m1"}, vectors, nil
	case "session/cancel":
		e.Foreground = false
		return Object{}, []Object{{"sessionUpdate": "state_update", "state": "idle", "stopReason": "cancelled", "background": []any{Object{"type": "tool_call", "toolCallId": "bg"}}}}, nil
	case "tool_call/cancel":
		if p["toolCallId"] != "t2" || !e.ToolCancelable {
			return nil, nil, invalid()
		}
		e.ToolCancelable = false
		return Object{}, []Object{{"sessionUpdate": "tool_call_update", "toolCallId": "t2", "status": "completed", "capabilities": nil}}, nil
	case "terminal/cancel":
		if p["terminalId"] != "term2" || !e.TerminalCancelable {
			return nil, nil, invalid()
		}
		e.TerminalCancelable = false
		return Object{}, []Object{{"sessionUpdate": "terminal_update", "terminalId": "term2", "exitStatus": Object{"exitCode": 130}, "capabilities": nil}}, nil
	default:
		return nil, nil, acp.NewMethodNotFound(method)
	}
}
func (e *Engine) missing() *acp.RequestError {
	if e.Version == 2 {
		return &acp.RequestError{Code: -32002, Message: "Session not found"}
	}
	return invalid()
}

// Decode through the generated selected profile before application handling.
func Decode(version int, method string, raw json.RawMessage) (Object, *acp.RequestError) {
	var target any
	if version == 2 {
		switch method {
		case "initialize":
			target = new(v2.InitializeRequest)
		case "session/new":
			target = new(v2.NewSessionRequest)
		case "session/resume":
			target = new(v2.ResumeSessionRequest)
		case "session/prompt":
			target = new(v2.PromptRequest)
		case "session/list":
			target = new(v2.ListSessionsRequest)
		case "session/close":
			target = new(v2.CloseSessionRequest)
		case "session/delete":
			target = new(v2.DeleteSessionRequest)
		case "tool_call/cancel":
			target = new(v2.CancelToolCallRequest)
		case "terminal/cancel":
			target = new(v2.CancelTerminalRequest)
		case "auth/login":
			target = new(v2.LoginAuthRequest)
		case "auth/logout":
			target = new(v2.LogoutAuthRequest)
		case "session/set_config_option":
			target = new(v2.SetSessionConfigOptionRequest)
		case "session/cancel":
			target = new(v2.CancelSessionNotification)
		}
	} else {
		switch method {
		case "initialize":
			target = new(acp.InitializeRequest)
		case "session/new":
			target = new(acp.NewSessionRequest)
		case "session/load":
			target = new(acp.LoadSessionRequest)
		case "session/resume":
			target = new(acp.ResumeSessionRequest)
		}
	}
	if target != nil {
		if err := json.Unmarshal(raw, target); err != nil {
			return nil, invalid()
		}
		if valid, ok := target.(interface{ Validate() error }); ok && valid.Validate() != nil {
			return nil, invalid()
		}
	}
	var p Object
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil, invalid()
	}
	return p, nil
}

func Serve(version int, profile, updatesPath string) error {
	engine, err := New(version, profile, updatesPath)
	if err != nil {
		return err
	}
	var reverse []struct {
		Method   string `json:"method"`
		Params   any    `json:"params"`
		Response any    `json:"response"`
		Notify   bool   `json:"notify"`
	}
	if err := Read(filepath.Join(filepath.Dir(updatesPath), "reverse.json"), &reverse); err != nil {
		return err
	}
	var conn *acp.Connection
	sendUpdate := func(ctx context.Context, sessionID any, update Object) error {
		if version == 2 {
			raw, err := json.Marshal(update)
			if err != nil {
				return err
			}
			var typed v2.SessionUpdate
			if err := json.Unmarshal(raw, &typed); err != nil {
				return err
			}
			id, _ := sessionID.(string)
			return conn.SendNotification(ctx, "session/update", v2.UpdateSessionNotification{SessionId: v2.SessionId(id), Update: typed})
		}
		return conn.SendNotification(ctx, "session/update", Object{"sessionId": sessionID, "update": update})
	}
	handler := func(ctx context.Context, method string, raw json.RawMessage) (any, *acp.RequestError) {
		p, err := Decode(version, method, raw)
		if err != nil {
			return nil, err
		}
		if version == 2 && method == "session/prompt" {
			blocks, _ := p["prompt"].([]any)
			if len(blocks) > 0 && object(blocks[0])["text"] == "request-cancel" {
				if err := sendUpdate(ctx, p["sessionId"], Object{"sessionUpdate": "notice", "severity": "info", "title": "request-ready"}); err != nil {
					return nil, acp.NewInternalError(nil)
				}
				<-ctx.Done()
				engine.Lock()
				engine.RequestCancelled = true
				engine.Unlock()
				ackCtx, ackCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer ackCancel()
				_ = sendUpdate(ackCtx, p["sessionId"], Object{"sessionUpdate": "notice", "severity": "info", "title": "request-cancelled"})
				return nil, &acp.RequestError{Code: -32800, Message: "Request cancelled"}
			}
		}
		result, updates, err := engine.Handle(method, p)
		if err != nil {
			return nil, err
		}
		for _, update := range updates {
			if err := sendUpdate(ctx, p["sessionId"], update); err != nil {
				return nil, acp.NewInternalError(nil)
			}
			if update["state"] == "requires_action" {
				for _, r := range reverse {
					if r.Notify {
						if err := conn.SendNotification(ctx, r.Method, r.Params); err != nil {
							return nil, acp.NewInternalError(err.Error())
						}
						continue
					}
					response, err := acp.SendRequest[any](conn, ctx, r.Method, r.Params)
					if err != nil {
						return nil, acp.NewInternalError(err.Error())
					}
					if err := Match(r.Response, response); err != nil {
						return nil, acp.NewInternalError(err.Error())
					}
				}
			}

		}
		return result, nil
	}
	var route acp.MethodHandler = handler
	if version == 2 {
		router := &v2.ProtocolRouter{V2: handler}
		route = router.Handle
	}
	opts := acp.ConnectionOptions{RejectBatch: version == 2}
	if version == 2 {
		opts.AcceptRequestID = v2.IsValidRequestID
	}
	conn, err = acp.NewUnstartedConnection(route, os.Stdout, os.Stdin, opts)
	if err != nil {
		return err
	}
	conn.Start()
	defer func() { _ = conn.Close() }()
	return conn.Wait(context.Background())
}

type Step struct {
	Method  string   `json:"method"`
	Params  any      `json:"params"`
	Expect  any      `json:"expect"`
	Error   int      `json:"error"`
	Notify  bool     `json:"notify"`
	Cancel  bool     `json:"cancel"`
	Updates []Object `json:"updates"`
}
type Case struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Profile string `json:"profile"`
	Steps   []Step `json:"steps"`
}

// Match uses object subsets and exact arrays, scalars and explicit null.
func Match(want, got any) error {
	switch w := want.(type) {
	case map[string]any:
		g := object(got)
		if g == nil {
			return fmt.Errorf("expected object, got %T", got)
		}
		for k, v := range w {
			actual, ok := g[k]
			if !ok {
				return fmt.Errorf("missing %s", k)
			}
			if err := Match(v, actual); err != nil {
				return fmt.Errorf("%s: %w", k, err)
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Errorf("array length mismatch")
		}
		for i, v := range w {
			if err := Match(v, g[i]); err != nil {
				return fmt.Errorf("[%d]: %w", i, err)
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Errorf("want %v got %v", want, got)
		}
	}
	return nil
}
func Run(ctx context.Context, conn *acp.Connection, c Case, updates *[]Object, mu *sync.Mutex) ([]string, error) {
	trace := []string{}
	offset := 0
	for i, s := range c.Steps {
		var got any
		var err error
		if s.Cancel {
			requestCtx, cancel := context.WithCancel(ctx)
			done := make(chan error, 1)
			go func() { _, err := acp.SendRequest[any](conn, requestCtx, s.Method, s.Params); done <- err }()
			for {
				mu.Lock()
				ready := len(*updates) > offset
				mu.Unlock()
				if ready {
					break
				}
				select {
				case <-ctx.Done():
					cancel()
					return trace, ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
			cancel()
			select {
			case err = <-done:
			case <-ctx.Done():
				return trace, ctx.Err()
			}
		} else if s.Notify {
			err = conn.SendNotification(ctx, s.Method, s.Params)
		} else {
			got, err = acp.SendRequest[any](conn, ctx, s.Method, s.Params)
		}
		if s.Error != 0 {
			re, ok := err.(*acp.RequestError)
			if !ok || re.Code != s.Error {
				return trace, fmt.Errorf("step %d %s: expected error %d, got %v", i, s.Method, s.Error, err)
			}
		} else {
			if err != nil {
				return trace, fmt.Errorf("step %d %s: %w", i, s.Method, err)
			}
			if !s.Notify {
				if err := Match(s.Expect, got); err != nil {
					return trace, fmt.Errorf("step %d %s: %w", i, s.Method, err)
				}
			}
		}
		for j, want := range s.Updates {
			for {
				mu.Lock()
				n := len(*updates)
				var actual Object
				if offset+j < n {
					actual = (*updates)[offset+j]
				}
				mu.Unlock()
				if actual != nil {
					if err := Match(want, actual); err != nil {
						return trace, fmt.Errorf("step %d update %d: %w", i, j, err)
					}
					break
				}
				select {
				case <-ctx.Done():
					return trace, ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
		}
		offset += len(s.Updates)
		trace = append(trace, fmt.Sprintf("%d:%s:ok", i, s.Method))
	}
	return trace, nil
}

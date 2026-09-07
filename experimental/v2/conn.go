package v2

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"

	acp "github.com/caelis-labs/acp-go-sdk"
)

// AgentSideConnection is the experimental v2 agent view of a connection.
type AgentSideConnection struct {
	conn  *acp.Connection
	agent Agent
}

// ClientSideConnection is the experimental v2 client view of a connection.
type ClientSideConnection struct {
	conn   *acp.Connection
	client Client
}

// NewAgentSideConnection binds a v2 Agent to a JSON-RPC connection.
func NewAgentSideConnection(agent Agent, peerInput io.Writer, peerOutput io.Reader) (*AgentSideConnection, error) {
	return NewAgentSideConnectionWithOptions(agent, peerInput, peerOutput, acp.ConnectionOptions{})
}

// NewAgentSideConnectionWithOptions creates a bounded v2 agent-side connection.
func NewAgentSideConnectionWithOptions(agent Agent, peerInput io.Writer, peerOutput io.Reader, opts acp.ConnectionOptions) (*AgentSideConnection, error) {
	asc := &AgentSideConnection{agent: agent}
	conn, err := acp.NewUnstartedConnection(asc.handle, peerInput, peerOutput, opts)
	if err != nil {
		return nil, err
	}
	asc.conn = conn
	conn.Start()
	return asc, nil
}

// NewClientSideConnection binds a v2 Client to a JSON-RPC connection.
func NewClientSideConnection(client Client, peerInput io.Writer, peerOutput io.Reader) (*ClientSideConnection, error) {
	return NewClientSideConnectionWithOptions(client, peerInput, peerOutput, acp.ConnectionOptions{})
}

// NewClientSideConnectionWithOptions creates a bounded v2 client-side connection.
func NewClientSideConnectionWithOptions(client Client, peerInput io.Writer, peerOutput io.Reader, opts acp.ConnectionOptions) (*ClientSideConnection, error) {
	csc := &ClientSideConnection{client: client}
	conn, err := acp.NewUnstartedConnection(csc.handle, peerInput, peerOutput, opts)
	if err != nil {
		return nil, err
	}
	csc.conn = conn
	conn.Start()
	return csc, nil
}

func (c *AgentSideConnection) Done() <-chan struct{} { return c.conn.Done() }
func (c *AgentSideConnection) Close() error          { return c.conn.Close() }
func (c *AgentSideConnection) Wait(ctx context.Context) error {
	return c.conn.Wait(ctx)
}
func (c *AgentSideConnection) Err() error { return c.conn.Err() }
func (c *AgentSideConnection) SetLogger(l *slog.Logger) {
	c.conn.SetLogger(l)
}

func (c *ClientSideConnection) Done() <-chan struct{} { return c.conn.Done() }
func (c *ClientSideConnection) Close() error          { return c.conn.Close() }
func (c *ClientSideConnection) Wait(ctx context.Context) error {
	return c.conn.Wait(ctx)
}
func (c *ClientSideConnection) Err() error { return c.conn.Err() }
func (c *ClientSideConnection) SetLogger(l *slog.Logger) {
	c.conn.SetLogger(l)
}

func (c *AgentSideConnection) SessionUpdate(ctx context.Context, params UpdateSessionNotification) error {
	return c.conn.SendNotification(ctx, ClientMethodSessionUpdate, params)
}

func (c *AgentSideConnection) RequestPermission(ctx context.Context, params RequestPermissionRequest) (RequestPermissionResponse, error) {
	return acp.SendRequest[RequestPermissionResponse](c.conn, ctx, ClientMethodSessionRequestPermission, params)
}

func (c *ClientSideConnection) Initialize(ctx context.Context, params InitializeRequest) (InitializeResponse, error) {
	return acp.SendRequest[InitializeResponse](c.conn, ctx, AgentMethodInitialize, params)
}

func (c *ClientSideConnection) NewSession(ctx context.Context, params NewSessionRequest) (NewSessionResponse, error) {
	return acp.SendRequest[NewSessionResponse](c.conn, ctx, AgentMethodSessionNew, params)
}

func (c *ClientSideConnection) ResumeSession(ctx context.Context, params ResumeSessionRequest) (ResumeSessionResponse, error) {
	return acp.SendRequest[ResumeSessionResponse](c.conn, ctx, AgentMethodSessionResume, params)
}

func (c *ClientSideConnection) ListSessions(ctx context.Context, params ListSessionsRequest) (ListSessionsResponse, error) {
	return acp.SendRequest[ListSessionsResponse](c.conn, ctx, AgentMethodSessionList, params)
}

func (c *ClientSideConnection) CloseSession(ctx context.Context, params CloseSessionRequest) (CloseSessionResponse, error) {
	return acp.SendRequest[CloseSessionResponse](c.conn, ctx, AgentMethodSessionClose, params)
}

func (c *ClientSideConnection) Prompt(ctx context.Context, params PromptRequest) (PromptResponse, error) {
	return acp.SendRequest[PromptResponse](c.conn, ctx, AgentMethodSessionPrompt, params)
}

func (c *ClientSideConnection) Cancel(ctx context.Context, params CancelSessionNotification) error {
	return c.conn.SendNotification(ctx, AgentMethodSessionCancel, params)
}

func (c *ClientSideConnection) LoginAuth(ctx context.Context, params LoginAuthRequest) (LoginAuthResponse, error) {
	return acp.SendRequest[LoginAuthResponse](c.conn, ctx, AgentMethodAuthLogin, params)
}

func (c *ClientSideConnection) LogoutAuth(ctx context.Context, params LogoutAuthRequest) (LogoutAuthResponse, error) {
	return acp.SendRequest[LogoutAuthResponse](c.conn, ctx, AgentMethodAuthLogout, params)
}

func (c *ClientSideConnection) DeleteSession(ctx context.Context, params DeleteSessionRequest) (DeleteSessionResponse, error) {
	return acp.SendRequest[DeleteSessionResponse](c.conn, ctx, AgentMethodSessionDelete, params)
}

func (c *ClientSideConnection) SetSessionConfigOption(ctx context.Context, params SetSessionConfigOptionRequest) (SetSessionConfigOptionResponse, error) {
	return acp.SendRequest[SetSessionConfigOptionResponse](c.conn, ctx, AgentMethodSessionSetConfigOption, params)
}

func (c *AgentSideConnection) CreateElicitation(ctx context.Context, params CreateElicitationRequest) (CreateElicitationResponse, error) {
	return acp.SendRequest[CreateElicitationResponse](c.conn, ctx, ClientMethodElicitationCreate, params)
}

func (c *AgentSideConnection) CompleteElicitation(ctx context.Context, params CompleteElicitationNotification) error {
	return c.conn.SendNotification(ctx, ClientMethodElicitationComplete, params)
}

func (a *AgentSideConnection) handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	switch method {
	case AgentMethodInitialize:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[InitializeRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		if _, err := SelectProtocolVersion(req.ProtocolVersion); err != nil {
			return nil, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
		resp, err := a.agent.Initialize(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionNew:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[NewSessionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := a.agent.NewSession(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionPrompt:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[PromptRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := a.agent.Prompt(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionCancel:
		if err := requireInboundKind(ctx, acp.InboundNotification, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[CancelSessionNotification](params)
		if reqErr != nil {
			return nil, reqErr
		}
		if err := a.agent.Cancel(ctx, req); err != nil {
			return nil, toRequestError(err)
		}
		return nil, nil
	case AgentMethodSessionResume:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		resumer, ok := a.agent.(AgentSessionResumer)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[ResumeSessionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := resumer.ResumeSession(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionList:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		lister, ok := a.agent.(AgentSessionLister)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[ListSessionsRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := lister.ListSessions(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionClose:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		closer, ok := a.agent.(AgentSessionCloser)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[CloseSessionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := closer.CloseSession(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionDelete:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		deleter, ok := a.agent.(AgentSessionDeleter)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[DeleteSessionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := deleter.DeleteSession(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodAuthLogin:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[LoginAuthRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := a.agent.LoginAuth(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodAuthLogout:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[LogoutAuthRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := a.agent.LogoutAuth(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case AgentMethodSessionSetConfigOption:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		cfg, ok := a.agent.(AgentSessionConfig)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[SetSessionConfigOptionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := cfg.SetSessionConfigOption(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	default:
		return nil, acp.NewMethodNotFound(method)
	}
}

func (c *ClientSideConnection) handle(ctx context.Context, method string, params json.RawMessage) (any, *acp.RequestError) {
	switch method {
	case ClientMethodSessionUpdate:
		if err := requireInboundKind(ctx, acp.InboundNotification, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[UpdateSessionNotification](params)
		if reqErr != nil {
			return nil, reqErr
		}
		if err := c.client.SessionUpdate(ctx, req); err != nil {
			return nil, toRequestError(err)
		}
		return nil, nil
	case ClientMethodSessionRequestPermission:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		req, reqErr := decodeParams[RequestPermissionRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := c.client.RequestPermission(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil

	case ClientMethodElicitationCreate:
		if err := requireInboundKind(ctx, acp.InboundRequest, method); err != nil {
			return nil, err
		}
		client, ok := c.client.(ClientElicitation)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[CreateElicitationRequest](params)
		if reqErr != nil {
			return nil, reqErr
		}
		resp, err := client.CreateElicitation(ctx, req)
		if err != nil {
			return nil, toRequestError(err)
		}
		return resp, nil
	case ClientMethodElicitationComplete:
		if err := requireInboundKind(ctx, acp.InboundNotification, method); err != nil {
			return nil, err
		}
		client, ok := c.client.(ClientElicitationCompletion)
		if !ok {
			return nil, acp.NewMethodNotFound(method)
		}
		req, reqErr := decodeParams[CompleteElicitationNotification](params)
		if reqErr != nil {
			return nil, reqErr
		}
		if err := client.CompleteElicitation(ctx, req); err != nil {
			return nil, toRequestError(err)
		}
		return nil, nil

	default:
		return nil, acp.NewMethodNotFound(method)
	}
}

type validator interface {
	Validate() error
}

func decodeParams[T any](params json.RawMessage) (T, *acp.RequestError) {
	var value T
	if err := json.Unmarshal(params, &value); err != nil {
		return value, acp.NewInvalidParams(map[string]any{"error": err.Error()})
	}
	if v, ok := any(&value).(validator); ok {
		if err := v.Validate(); err != nil {
			return value, acp.NewInvalidParams(map[string]any{"error": err.Error()})
		}
	}
	return value, nil
}

// Keep protocol errors intact across the experimental typed dispatch boundary.
func toRequestError(err error) *acp.RequestError {
	var requestError *acp.RequestError
	if errors.As(err, &requestError) {
		return requestError
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return acp.NewRequestCancelled(map[string]any{"error": err.Error()})
	}
	return acp.NewInternalError(map[string]any{"error": err.Error()})
}

func requireInboundKind(ctx context.Context, want acp.InboundKind, method string) *acp.RequestError {
	info, ok := acp.InboundInfoFromContext(ctx)
	if !ok {
		return acp.NewInternalError(map[string]any{"error": "ACP inbound message metadata is unavailable"})
	}
	if info.Kind != want {
		return acp.NewMethodNotFound(method)
	}
	return nil
}

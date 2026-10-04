package v2

import (
	"context"
	"encoding/json"
	"net"
	"reflect"
	"testing"
	"time"
)

func TestInitialAvailableCommandsDecode(t *testing.T) {
	const first = `{"name":"first","description":"First command"}`
	const last = `{"name":"last","description":"Last command","input":{"type":"text","hint":"Query"}}`
	for _, method := range []string{"new", "resume"} {
		t.Run(method, func(t *testing.T) {
			for _, tc := range []struct {
				name  string
				field string
				want  []string
			}{
				{"omitted", "", nil},
				{"empty", `,"availableCommands":[]`, nil},
				{"null", `,"availableCommands": null`, nil},
				{"object", `,"availableCommands":{}`, nil},
				{"string", `,"availableCommands":"bad"`, nil},
				{"number", `,"availableCommands":42`, nil},
				{"boolean", `,"availableCommands":true`, nil},
				{"valid", `,"availableCommands":[` + first + `,` + last + `]`, []string{"first", "last"}},
				{"invalid-only", `,"availableCommands":[null,{},[],false,42,"bad",{"name":"missing-description"}]`, nil},
				{"mixed", `,"availableCommands":[` + first + `,null,{},[],false,42,"bad",{"name":null,"description":"bad"},{"name":42,"description":"bad"},{"name":"bad-input","description":"bad","input":{"type":"text"}},` + last + `]`, []string{"first", "last"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					stale := []AvailableCommand{{Name: "stale", Description: "Must be reset"}}
					var response any
					if method == "new" {
						response = &NewSessionResponse{AvailableCommands: stale}
					} else {
						response = &ResumeSessionResponse{AvailableCommands: stale}
					}
					input := `{"sessionId":"s","_meta":{"sequence":9007199254740993}` + tc.field + `}`
					if err := json.Unmarshal([]byte(input), response); err != nil {
						t.Fatal(err)
					}
					var commands []AvailableCommand
					switch r := response.(type) {
					case *NewSessionResponse:
						commands = r.AvailableCommands
						if r.SessionId != "s" {
							t.Fatal("session ID lost")
						}
					case *ResumeSessionResponse:
						commands = r.AvailableCommands
					}
					var names []string
					for _, command := range commands {
						names = append(names, command.Name)
					}
					if !reflect.DeepEqual(names, tc.want) {
						t.Fatalf("commands = %v, want %v", names, tc.want)
					}
					encoded, err := json.Marshal(response)
					if err != nil {
						t.Fatal(err)
					}
					var object map[string]json.RawMessage
					if err := json.Unmarshal(encoded, &object); err != nil {
						t.Fatal(err)
					}
					if raw, ok := object["availableCommands"]; ok && (len(raw) == 0 || raw[0] != '[') {
						t.Fatalf("sender emitted non-array commands: %s", encoded)
					}
					if string(object["_meta"]) != `{"sequence":9007199254740993}` {
						t.Fatalf("metadata lost: %s", encoded)
					}
				})
			}
		})
	}
}

func TestInitialAvailableCommandsPreserveExtensions(t *testing.T) {
	for _, inputType := range []string{"_custom", "future"} {
		command := `{"_meta":{"sequence":9007199254740993},"description":"Extension","input":{"type":"` + inputType + `","payload":{"sequence":9007199254740993},"_meta":{"opaque":true}},"name":"extension"}`
		for _, response := range []any{&NewSessionResponse{}, &ResumeSessionResponse{}} {
			if err := json.Unmarshal([]byte(`{"sessionId":"s","availableCommands":[`+command+`]}`), response); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			var object struct {
				Commands []json.RawMessage `json:"availableCommands"`
			}
			if err := json.Unmarshal(encoded, &object); err != nil {
				t.Fatal(err)
			}
			if len(object.Commands) != 1 || string(object.Commands[0]) != command {
				t.Fatalf("extension changed: %s", encoded)
			}
		}
	}
}

func TestInitialCommandsRecoveryDoesNotRelaxOtherFields(t *testing.T) {
	for _, input := range []string{`{}`, `{"sessionId":null}`, `{"sessionId":42}`, `{"sessionId":"s","configOptions":"bad"}`, `{"sessionId":"s","_meta":42}`, `{"sessionId":"s","availableCommands":[}`} {
		var response NewSessionResponse
		if err := json.Unmarshal([]byte(input), &response); err == nil {
			t.Fatalf("accepted invalid new session: %s", input)
		}
	}
	for _, input := range []string{`{"configOptions":"bad"}`, `{"_meta":42}`, `{"availableCommands":[}`} {
		var response ResumeSessionResponse
		if err := json.Unmarshal([]byte(input), &response); err == nil {
			t.Fatalf("accepted invalid resume session: %s", input)
		}
	}
}

type commandsAgent struct {
	Agent
	commands []AvailableCommand
}

func (a commandsAgent) NewSession(context.Context, NewSessionRequest) (NewSessionResponse, error) {
	return NewSessionResponse{SessionId: "s", AvailableCommands: a.commands}, nil
}

func (a commandsAgent) ResumeSession(context.Context, ResumeSessionRequest) (ResumeSessionResponse, error) {
	return ResumeSessionResponse{AvailableCommands: a.commands}, nil
}

type commandsClient struct {
	Client
	updates chan []AvailableCommand
}

func (c commandsClient) SessionUpdate(_ context.Context, params UpdateSessionNotification) error {
	if update := params.Update.AvailableCommandsUpdate; update != nil {
		c.updates <- update.AvailableCommands
	}
	return nil
}

func TestV2InitialCommandsAndReplacementNotifications(t *testing.T) {
	for _, method := range []string{"new", "resume"} {
		t.Run(method, func(t *testing.T) {
			a, b := net.Pipe()
			initial := []AvailableCommand{{Name: "first", Description: "First"}, {Name: "second", Description: "Second"}}
			agent, err := NewAgentSideConnection(commandsAgent{commands: initial}, a, a)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = agent.Close() }()
			updates := make(chan []AvailableCommand, 2)
			client, err := NewClientSideConnection(commandsClient{updates: updates}, b, b)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var commands []AvailableCommand
			if method == "new" {
				response, err := client.NewSession(ctx, NewSessionRequest{Cwd: "/tmp"})
				if err != nil {
					t.Fatal(err)
				}
				commands = response.AvailableCommands
			} else {
				response, err := client.ResumeSession(ctx, ResumeSessionRequest{SessionId: "s", Cwd: "/tmp"})
				if err != nil {
					t.Fatal(err)
				}
				commands = response.AvailableCommands
			}
			if !reflect.DeepEqual(commands, initial) {
				t.Fatalf("initial commands = %+v", commands)
			}
			for _, replacement := range [][]AvailableCommand{{{Name: "replacement", Description: "Replacement"}}, {}} {
				if err := agent.SessionUpdate(ctx, UpdateSessionNotification{SessionId: "s", Update: SessionUpdate{AvailableCommandsUpdate: &SessionAvailableCommandsUpdate{AvailableCommands: replacement}}}); err != nil {
					t.Fatal(err)
				}
			}
			for _, want := range []int{1, 0} {
				select {
				case commands = <-updates:
					if len(commands) != want || (want == 1 && commands[0].Name != "replacement") {
						t.Fatalf("replacement commands = %+v, want length %d", commands, want)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		})
	}
}

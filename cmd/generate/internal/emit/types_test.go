package emit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/caelis-labs/acp-go-sdk/cmd/generate/internal/load"
)

func TestEmitUnionPreservesSiblingPropertiesForReferencedVariants(t *testing.T) {
	schema := &load.Schema{Defs: map[string]*load.Definition{
		"Scope": {
			Type: "object",
			Properties: map[string]*load.Definition{
				"sessionId": {Type: "string"},
			},
			Required: []string{"sessionId"},
		},
	}}
	parent := &load.Definition{
		Type: "object",
		Properties: map[string]*load.Definition{
			"shared": {Type: "string"},
		},
		Required: []string{"shared"},
	}
	variants := []*load.Definition{{
		Title: "Session",
		AllOf: []*load.Definition{{Ref: "#/$defs/Scope"}},
	}}
	file := NewFile("acp")
	emitUnion(file, "Hybrid", schema, parent, variants, false, map[string]bool{
		"Hybrid": true,
		"Scope":  true,
	})
	var output bytes.Buffer
	if err := file.Render(&output); err != nil {
		t.Fatal(err)
	}
	generated := output.String()
	for _, want := range []string{
		"type HybridSession struct",
		"SessionId string",
		"Shared",
		`json:"shared"`,
		`m["sessionId"]`,
		`m["shared"]`,
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated code missing %q:\n%s", want, generated)
		}
	}
}

func TestEmitUnionPreservesForwardCompatibleRawVariant(t *testing.T) {
	schema := &load.Schema{Defs: map[string]*load.Definition{
		"Scope": {
			Type: "object",
			Properties: map[string]*load.Definition{
				"sessionId": {Type: "string"},
			},
			Required: []string{"sessionId"},
		},
	}}
	variant := &load.Definition{
		Title:                "other",
		Description:          "An open forward-compatible variant.",
		Type:                 "object",
		AdditionalProperties: true,
		Properties: map[string]*load.Definition{
			"mode": {Type: "string"},
		},
		Required: []string{"mode"},
		AnyOf: []*load.Definition{{
			AllOf: []*load.Definition{{Ref: "#/$defs/Scope"}},
		}},
	}
	file := NewFile("acp")
	emitUnion(file, "Future", schema, &load.Definition{}, []*load.Definition{variant}, false, map[string]bool{"Future": true})
	var output bytes.Buffer
	if err := file.Render(&output); err != nil {
		t.Fatal(err)
	}
	generated := output.String()
	for _, want := range []string{
		"type FutureOther = json.RawMessage",
		`m["sessionId"]`,
		"return _b, nil",
		"*u = Future{}",
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated code missing %q:\n%s", want, generated)
		}
	}
}

func TestPrimitiveJenTypeHonorsIntegerFormats(t *testing.T) {
	tests := map[string]string{
		"int32":  "int32",
		"int64":  "int64",
		"uint16": "uint16",
		"uint32": "uint32",
		"uint64": "uint64",
		"":       "int64",
	}
	for format, want := range tests {
		t.Run(format, func(t *testing.T) {
			file := NewFile("acp")
			file.Type().Id("Value").Add(primitiveJenType(&load.Definition{Type: "integer", Format: format}))
			var output bytes.Buffer
			if err := file.Render(&output); err != nil {
				t.Fatal(err)
			}
			if got := output.String(); !strings.Contains(got, "type Value "+want) {
				t.Fatalf("generated code missing formatted integer %q:\n%s", want, got)
			}
		})
	}
}

func TestEmitRequiredObjectUnmarshalChecksPresenceAndNull(t *testing.T) {
	definition := &load.Definition{
		Type: "object",
		Properties: map[string]*load.Definition{
			"count": {Type: "integer", Format: "uint64"},
			"note":  {Type: []any{"string", "null"}},
		},
		Required: []string{"count", "note"},
	}
	file := NewFile("acp")
	file.Type().Id("Payload").Struct(
		Id("Count").Uint64().Tag(map[string]string{"json": "count"}),
		Id("Note").Op("*").String().Tag(map[string]string{"json": "note"}),
	)
	emitRequiredObjectUnmarshal(file, "Payload", &load.Schema{}, definition)
	var output bytes.Buffer
	if err := file.Render(&output); err != nil {
		t.Fatal(err)
	}
	generated := output.String()
	for _, want := range []string{
		"*v = Payload{}",
		`m["count"]`,
		"count is required",
		"count must not be null",
		`m["note"]`,
	} {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated code missing %q:\n%s", want, generated)
		}
	}
	if strings.Contains(generated, "note must not be null") {
		t.Fatalf("nullable required property incorrectly rejects null:\n%s", generated)
	}
}

func TestEmitUnionChecksPrimitiveConsts(t *testing.T) {
	variants := []*load.Definition{
		{Title: "Known", Type: "integer", Const: float64(-1)},
		{Title: "Other", Type: "integer"},
	}
	file := NewFile("acp")
	emitUnion(file, "Code", &load.Schema{}, &load.Definition{}, variants, false, map[string]bool{"Code": true})
	var output bytes.Buffer
	if err := file.Render(&output); err != nil {
		t.Fatal(err)
	}
	generated := output.String()
	if !strings.Contains(generated, "v == CodeKnown(-1.0)") {
		t.Fatalf("numeric const guard missing:\n%s", generated)
	}
}

func TestIncludesNullAnyOfWithoutTopLevelType(t *testing.T) {
	definition := &load.Definition{AnyOf: []*load.Definition{
		{Ref: "#/$defs/ToolCallId"},
		{Type: "null"},
	}}
	if !includesNull(definition) {
		t.Fatal("nullable anyOf was not recognized")
	}
}

func TestNullablePresencePropertiesRequireExplicitClearSemantics(t *testing.T) {
	properties := map[string]*load.Definition{
		"name": {
			Description: "Omission means no change, `null` clears the name, and a string replaces it.",
			Type:        []any{"string", "null"},
		},
		"v1Name": {
			Description: "Omitting it or sending `null` both mean that the existing name is left unchanged.",
			Type:        []any{"string", "null"},
		},
		"title": {
			Description: "Human-readable title. Set to null to clear.",
			Type:        []any{"string", "null"},
		},
		"ordinary": {
			Description: "An ordinary nullable field.",
			Type:        []any{"string", "null"},
		},
		"required": {
			Description: "Set to null to clear.",
			Type:        []any{"string", "null"},
		},
	}
	got := nullablePresenceProperties(properties, map[string]struct{}{"required": {}})
	if len(got) != 2 || got[0].propName != "name" || got[0].presentName != "hasName" || got[1].propName != "title" || got[1].presentName != "hasTitle" {
		t.Fatalf("nullable presence properties = %#v", got)
	}
}

func TestInitialCommandsRecoveryScope(t *testing.T) {
	for _, method := range []string{"session/new", "session/resume", "session/update"} {
		for _, enabled := range []bool{false, true} {
			prop := &load.Definition{Type: "array", Items: &load.Definition{Ref: "#/$defs/AvailableCommand"}, DeserializeDefaultOnError: enabled, DeserializeSkipInvalidItems: true}
			definition := &load.Definition{XSide: "agent", XMethod: method, Properties: map[string]*load.Definition{"availableCommands": prop, "configOptions": prop}}
			want := enabled && method != "session/update"
			if got := initialCommandsProperty(definition); (got != nil) != want {
				t.Fatalf("%s markers=%v: recovery = %v, want %v", method, enabled, got != nil, want)
			}
			definition.Required = []string{"availableCommands"}
			if initialCommandsProperty(definition) != nil {
				t.Fatal("required field recovery enabled")
			}
		}
	}
}

func TestCompactionPatchPresenceIncludesCollections(t *testing.T) {
	properties := map[string]*load.Definition{
		"compactionId": {Type: "string"}, "status": {Type: "string"},
		"summary": {Type: []any{"array", "null"}, Items: &load.Definition{Ref: "#/$defs/ContentBlock"}},
		"error":   {Type: []any{"string", "null"}},
		"_meta":   {Type: []any{"object", "null"}, AdditionalProperties: true},
	}
	got := nullablePresenceProperties(properties, nil)
	if len(got) != 3 || got[0].propName != "_meta" || got[0].indirect || got[1].propName != "error" || !got[1].indirect || got[2].propName != "summary" || got[2].indirect {
		t.Fatalf("patch properties=%+v", got)
	}
}

func TestNestedUnionAlternativeProperties(t *testing.T) {
	schema := &load.Schema{Defs: map[string]*load.Definition{
		"Failure": {Type: "object", Properties: map[string]*load.Definition{"error": {Ref: "#/$defs/Error"}}},
	}}
	definition := &load.Definition{AnyOf: []*load.Definition{{AnyOf: []*load.Definition{{AllOf: []*load.Definition{{Ref: "#/$defs/Failure"}}}}}}}
	if unionAlternativeProperties(schema, definition)["error"] == nil {
		t.Fatal("nested failure details lost")
	}
}

func TestEmitUnionComposesNestedOpenUnion(t *testing.T) {
	for _, open := range []bool{false, true} {
		schema := &load.Schema{Defs: map[string]*load.Definition{
			"Inner": {AnyOf: []*load.Definition{{
				Title: "other", Type: "object", AdditionalProperties: open,
				Properties: map[string]*load.Definition{"reason": {Type: "string"}}, Required: []string{"reason"},
			}}},
		}}
		variant := &load.Definition{
			Type: "object", Properties: map[string]*load.Definition{"kind": {Type: "string", Const: "nested"}},
			Required: []string{"kind"}, AllOf: []*load.Definition{{Ref: "#/$defs/Inner"}},
		}
		file := NewFile("acp")
		emitUnion(file, "Outer", schema, &load.Definition{}, []*load.Definition{variant}, false, map[string]bool{"Outer": true, "Inner": true})
		var output bytes.Buffer
		if err := file.Render(&output); err != nil {
			t.Fatal(err)
		}
		generated := output.String()
		for _, fragment := range []string{"Inner Inner", "json.Marshal(v.Inner)", "json.Unmarshal(b, &a.Inner)"} {
			if strings.Contains(generated, fragment) != open {
				t.Fatalf("open=%v: nested composition %q:\n%s", open, fragment, generated)
			}
		}
		if open && strings.Contains(generated, "Reason *string") {
			t.Fatalf("nested union was flattened:\n%s", generated)
		}
	}
}

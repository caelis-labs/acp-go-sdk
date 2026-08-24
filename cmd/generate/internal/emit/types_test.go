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

package emit

import (
	"sort"

	"github.com/caelis-labs/acp-go-sdk/cmd/generate/internal/ir"
	"github.com/caelis-labs/acp-go-sdk/cmd/generate/internal/load"
	"github.com/caelis-labs/acp-go-sdk/cmd/generate/internal/util"
)

// Limit receiver recovery to the newly stabilized display fields. Older fields
// retain their established decoding behavior. Executable/session inputs are
// deliberately strict, including null elements in otherwise valid arrays.
func displayRecoveryProperties(def *load.Definition) []string {
	var names []string
	p := def.Properties
	switch {
	case def.XMethod == "initialize" && p["authMethods"] != nil:
		// Dropping an unusable terminal auth method must not turn it into
		// agent auth or prevent the client from using a valid sibling method.
		names = []string{"authMethods"}
	case p["compactionId"] != nil && p["status"] != nil:
		names = []string{"_meta", "error", "summary"}
	case p["severity"] != nil && p["title"] != nil:
		names = []string{"_meta", "description"}
	case p["compactionId"] != nil && p["content"] != nil:
		names = []string{"_meta"}
	case p["compaction"] != nil && p["notices"] != nil:
		names = []string{"compaction", "notices"}
	case p["stopReason"] != nil:
		names = []string{"stopReason", "error"}
	}
	var result []string
	for _, name := range names {
		if prop := p[name]; prop != nil && (prop.DeserializeDefaultOnError || ir.PrimaryType(prop) == "null") {
			result = append(result, name)
		}
	}
	return result
}

func emitDisplayRecoveryDecode(g *Group, def *load.Definition) {
	names := displayRecoveryProperties(def)
	if len(names) == 0 {
		g.If(List(Id("err")).Op(":=").Qual("encoding/json", "Unmarshal").Call(Id("b"), Op("&").Id("a")), Id("err").Op("!=").Nil()).Block(Return(Id("err")))
		return
	}
	fields := []Code{Id("Alias")}
	for _, name := range names {
		fields = append(fields, Id(util.ToExportedField(name)).Qual("encoding/json", "RawMessage").Tag(map[string]string{"json": name}))
	}
	g.Var().Id("raw").Struct(fields...)
	g.If(List(Id("err")).Op(":=").Qual("encoding/json", "Unmarshal").Call(Id("b"), Op("&").Id("raw")), Id("err").Op("!=").Nil()).Block(Return(Id("err")))
	g.Id("a").Op("=").Id("raw").Dot("Alias")
	for _, name := range names {
		prop := def.Properties[name]
		field := util.ToExportedField(name)
		g.BlockFunc(func(h *Group) {
			if ir.PrimaryType(prop) == "null" {
				return // The idle "none" variant ignores a malformed stop reason.
			}
			if ir.PrimaryType(prop) == "array" && prop.DeserializeSkipInvalidItems {
				h.Var().Id("items").Index().Qual("encoding/json", "RawMessage")
				h.If(Qual("encoding/json", "Unmarshal").Call(Id("raw").Dot(field), Op("&").Id("items")).Op("==").Nil().Op("&&").Id("items").Op("!=").Nil()).BlockFunc(func(array *Group) {
					array.Id("a").Dot(field).Op("=").Make(jenTypeFor(prop), Lit(0), Id("len").Call(Id("items")))
					array.For(List(Id("_"), Id("item")).Op(":=").Range().Id("items")).BlockFunc(func(loop *Group) {
						loop.Var().Id("value").Add(jenTypeFor(prop.Items))
						loop.If(Qual("encoding/json", "Unmarshal").Call(Id("item"), Op("&").Id("value")).Op("==").Nil()).Block(
							Id("a").Dot(field).Op("=").Append(Id("a").Dot(field), Id("value")),
						)
					})
				})
			} else {
				// Decode into a temporary: encoding/json may partially populate a
				// value before returning an error, which must not leak into recovery.
				h.Var().Id("value").Add(jenTypeForProperty(name, prop))
				h.If(Qual("encoding/json", "Unmarshal").Call(Id("raw").Dot(field), Op("&").Id("value")).Op("==").Nil()).Block(
					Id("a").Dot(field).Op("=").Id("value"),
				)
			}
		})
	}
}

func strictReceiverProperties(def *load.Definition) []string {
	p := def.Properties
	var names []string
	switch {
	case def.XMethod == "terminal/create":
		names = []string{"args", "env", "cwd", "outputByteLimit"}
	case def.XMethod == "fs/read_text_file":
		names = []string{"line", "limit"}
	case p["mcpServers"] != nil && p["cwd"] != nil:
		names = []string{"mcpServers", "additionalDirectories"}
	case p["args"] != nil && p["env"] != nil && (p["command"] != nil || p["id"] != nil):
		names = []string{"args", "env"}
	case p["url"] != nil && p["headers"] != nil && p["name"] != nil:
		names = []string{"headers"}
	}
	return names
}

// Official receivers use DefaultOnNull for action lists/maps even though the
// JSON Schema describes their non-null shape. Malformed non-null values remain
// errors; null is empty, and null elements are never silently coerced.
func receiverNullIsEmpty(def *load.Definition, name string) bool {
	for _, strict := range strictReceiverProperties(def) {
		if strict == name {
			prop := def.Properties[name]
			return prop != nil && (ir.PrimaryType(prop) == "array" || ir.PrimaryType(prop) == "object")
		}
	}
	return false
}

func emitReceiverNullDefaults(g *Group, def *load.Definition) {
	for _, name := range def.Required {
		if receiverNullIsEmpty(def, name) && ir.PrimaryType(def.Properties[name]) == "array" {
			field := util.ToExportedField(name)
			g.If(Id("a").Dot(field).Op("==").Nil()).Block(Id("a").Dot(field).Op("=").Make(jenTypeFor(def.Properties[name]), Lit(0)))
		}
	}
}

func emitReceiverPropertyChecks(g *Group, schema *load.Schema, def *load.Definition) {
	for _, name := range strictReceiverProperties(def) {
		prop := def.Properties[name]
		if prop == nil {
			continue
		}
		g.BlockFunc(func(h *Group) {
			h.List(Id("raw"), Id("ok")).Op(":=").Id("m").Index(Lit(name))
			h.If(Id("ok")).BlockFunc(func(present *Group) {
				emitStrictReceiverValue(present, schema, prop, Id("raw"), receiverNullIsEmpty(def, name) || includesNull(prop))
			})
		})
	}
	// minLength is currently used by the new Notice title. Apply its declared
	// constraint to both standalone objects and inherited union variants.
	keys := make([]string, 0, len(def.Properties))
	for name := range def.Properties {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		prop := def.Properties[name]
		if prop.MinLength <= 0 {
			continue
		}
		g.BlockFunc(func(h *Group) {
			h.If(List(Id("raw"), Id("ok")).Op(":=").Id("m").Index(Lit(name)), Id("ok")).Block(
				Var().Id("value").String(),
				If(Qual("encoding/json", "Unmarshal").Call(Id("raw"), Op("&").Id("value")).Op("!=").Nil().Op("||").Qual("unicode/utf8", "RuneCountInString").Call(Id("value")).Op("<").Lit(prop.MinLength)).Block(
					Return(Qual("fmt", "Errorf").Call(Lit(name+" is too short"))),
				),
			)
		})
	}
}

func needsReceiverDecode(def *load.Definition) bool {
	if len(strictReceiverProperties(def)) > 0 || len(displayRecoveryProperties(def)) > 0 {
		return true
	}
	for _, prop := range def.Properties {
		if prop.MinLength > 0 || ir.PrimaryType(prop) == "null" {
			return true
		}
	}
	return false
}

func emitStrictReceiverValue(g *Group, schema *load.Schema, prop *load.Definition, raw Code, allowNull bool) {
	isNull := Qual("bytes", "Equal").Call(Qual("bytes", "TrimSpace").Call(raw), Index().Byte().Parens(Lit("null")))
	check := func(h *Group) {
		h.If(isNull).Block(Return(Qual("errors", "New").Call(Lit("invalid null action value"))))
		switch ir.PrimaryType(prop) {
		case "array":
			h.Var().Id("items").Index().Qual("encoding/json", "RawMessage")
			h.If(List(Id("err")).Op(":=").Qual("encoding/json", "Unmarshal").Call(raw, Op("&").Id("items")), Id("err").Op("!=").Nil()).Block(Return(Id("err")))
			h.For(List(Id("_"), Id("item")).Op(":=").Range().Id("items")).BlockFunc(func(loop *Group) {
				emitStrictReceiverValue(loop, schema, prop.Items, Id("item"), false)
			})
		case "object":
			if additional, ok := prop.AdditionalProperties.(map[string]any); ok {
				if additional["type"] == "string" {
					h.Var().Id("items").Map(String()).Qual("encoding/json", "RawMessage")
					h.If(List(Id("err")).Op(":=").Qual("encoding/json", "Unmarshal").Call(raw, Op("&").Id("items")), Id("err").Op("!=").Nil()).Block(Return(Id("err")))
					h.For(List(Id("_"), Id("item")).Op(":=").Range().Id("items")).BlockFunc(func(loop *Group) {
						emitStrictReceiverValue(loop, schema, &load.Definition{Type: "string"}, Id("item"), false)
					})
					return
				}
			}
			fallthrough
		default:
			h.Var().Id("value").Add(jenTypeFor(prop))
			h.If(List(Id("err")).Op(":=").Qual("encoding/json", "Unmarshal").Call(raw, Op("&").Id("value")), Id("err").Op("!=").Nil()).Block(Return(Id("err")))
		}
	}
	if allowNull {
		g.If(Op("!").Add(isNull)).BlockFunc(check)
	} else {
		check(g)
	}
}

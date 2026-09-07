package emit

import jen "github.com/dave/jennifer/jen"

// Local aliases to avoid dot-importing jennifer while keeping concise calls.
type (
	Code  = jen.Code
	Dict  = jen.Dict
	Group = jen.Group
	File  = jen.File
)

// PackageName is the Go package written into generated files.
var PackageName = "acp"

var (
	NewFile       = jen.NewFile
	Id            = jen.Id
	Lit           = jen.Lit
	Line          = jen.Line
	Func          = jen.Func
	For           = jen.For
	Range         = jen.Range
	Return        = jen.Return
	Nil           = jen.Nil
	String        = jen.String
	Int           = jen.Int
	Float64       = jen.Float64
	Bool          = jen.Bool
	Any           = jen.Any
	Map           = jen.Map
	Struct        = jen.Struct
	Index         = jen.Index
	Qual          = jen.Qual
	Error         = jen.Error
	Case          = jen.Case
	Default       = jen.Default
	Defer         = jen.Defer
	Switch        = jen.Switch
	Var           = jen.Var
	If            = jen.If
	List          = jen.List
	Op            = jen.Op
	InterfaceFunc = jen.InterfaceFunc
	Comment       = jen.Comment
)

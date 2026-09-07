package v2

import "fmt"

// NullableFieldState reports whether an optional nullable ACP field was
// absent, explicitly null, or carried a value on the wire.
type NullableFieldState uint8

const (
	NullableFieldAbsent NullableFieldState = iota
	NullableFieldNull
	NullableFieldValue
)

func (s NullableFieldState) String() string {
	switch s {
	case NullableFieldAbsent:
		return "absent"
	case NullableFieldNull:
		return "null"
	case NullableFieldValue:
		return "value"
	default:
		return fmt.Sprintf("NullableFieldState(%d)", s)
	}
}

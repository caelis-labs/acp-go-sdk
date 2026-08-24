package acp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const (
	maxCanonicalJSONRPCIDKeyLen   = 4096
	maxCanonicalJSONRPCIDAbsExp10 = 4096
	maxPendingCancelRequests      = 1024
)

var (
	errInvalidJSONRPCNumericID  = errors.New("invalid json-rpc numeric id")
	errJSONRPCNumericIDTooLarge = errors.New("json-rpc numeric id too large")
)

func canonicalJSONRPCIDKey(raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return "", errors.New("empty json-rpc id")
	}

	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()

	var id any
	if err := dec.Decode(&id); err != nil {
		return "", err
	}

	// Ensure the id contains a single JSON value.
	var trailing any
	if err := dec.Decode(&trailing); err == nil {
		return "", errors.New("invalid json-rpc id: trailing data")
	} else if !errors.Is(err, io.EOF) {
		return "", err
	}

	switch v := id.(type) {
	case nil:
		return "null", nil
	case string:
		canon, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		return string(canon), nil
	case json.Number:
		return canonicalJSONRPCNumericIDKey(v)
	default:
		return "", errors.New("json-rpc id must be string, number, or null")
	}
}

func canonicalJSONRPCNumericIDKey(v json.Number) (string, error) {
	raw := strings.TrimSpace(v.String())
	if raw == "" {
		return "", errInvalidJSONRPCNumericID
	}

	negative, digits, exp10, err := parseJSONRPCNumericID(raw)
	if err != nil {
		return "", err
	}

	return formatCanonicalJSONRPCNumericID(negative, digits, exp10)
}

func parseJSONRPCNumericID(raw string) (negative bool, digits string, exp10 int, err error) {
	i := 0
	if raw[i] == '-' {
		negative = true
		i++
		if i >= len(raw) {
			return false, "", 0, errInvalidJSONRPCNumericID
		}
	}

	intStart := i
	switch {
	case raw[i] == '0':
		i++
		if i < len(raw) && isASCIIDigit(raw[i]) {
			return false, "", 0, errInvalidJSONRPCNumericID
		}
	case raw[i] >= '1' && raw[i] <= '9':
		for i < len(raw) && isASCIIDigit(raw[i]) {
			i++
		}
	default:
		return false, "", 0, errInvalidJSONRPCNumericID
	}
	intDigits := raw[intStart:i]

	fracDigits := ""
	if i < len(raw) && raw[i] == '.' {
		i++
		fracStart := i
		for i < len(raw) && isASCIIDigit(raw[i]) {
			i++
		}
		if fracStart == i {
			return false, "", 0, errInvalidJSONRPCNumericID
		}
		fracDigits = raw[fracStart:i]
	}

	exponent := 0
	if i < len(raw) && (raw[i] == 'e' || raw[i] == 'E') {
		i++
		if i >= len(raw) {
			return false, "", 0, errInvalidJSONRPCNumericID
		}

		exponentSign := 1
		if raw[i] == '+' || raw[i] == '-' {
			if raw[i] == '-' {
				exponentSign = -1
			}
			i++
			if i >= len(raw) {
				return false, "", 0, errInvalidJSONRPCNumericID
			}
		}

		exponentStart := i
		for i < len(raw) && isASCIIDigit(raw[i]) {
			i++
		}
		if exponentStart == i {
			return false, "", 0, errInvalidJSONRPCNumericID
		}

		exponentMagnitude, parseErr := parseBoundedInt(raw[exponentStart:i], maxCanonicalJSONRPCIDAbsExp10)
		if parseErr != nil {
			return false, "", 0, parseErr
		}
		exponent = exponentSign * exponentMagnitude
	}

	if i != len(raw) {
		return false, "", 0, errInvalidJSONRPCNumericID
	}

	digits = strings.TrimLeft(intDigits+fracDigits, "0")
	if digits == "" {
		return false, "", 0, nil
	}
	if len(digits) > maxCanonicalJSONRPCIDKeyLen {
		return false, "", 0, errJSONRPCNumericIDTooLarge
	}

	exp10 = exponent - len(fracDigits)
	return negative, digits, exp10, nil
}

func parseBoundedInt(raw string, max int) (int, error) {
	if raw == "" {
		return 0, errInvalidJSONRPCNumericID
	}

	value := 0
	for i := 0; i < len(raw); i++ {
		if !isASCIIDigit(raw[i]) {
			return 0, errInvalidJSONRPCNumericID
		}
		digit := int(raw[i] - '0')
		if value > (max-digit)/10 {
			return 0, errJSONRPCNumericIDTooLarge
		}
		value = value*10 + digit
	}
	return value, nil
}

func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}

func formatCanonicalJSONRPCNumericID(negative bool, digits string, exp10 int) (string, error) {
	for len(digits) > 0 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
		exp10++
	}

	if digits == "" {
		return "0", nil
	}

	sign := ""
	if negative {
		sign = "-"
	}

	if exp10 >= 0 {
		if exp10 > maxCanonicalJSONRPCIDKeyLen-len(digits) {
			return "", errJSONRPCNumericIDTooLarge
		}
		result := digits + strings.Repeat("0", exp10)
		if sign != "" {
			result = sign + result
		}
		if len(result) > maxCanonicalJSONRPCIDKeyLen+len(sign) {
			return "", errJSONRPCNumericIDTooLarge
		}
		return result, nil
	}

	scale := -exp10
	if scale > maxCanonicalJSONRPCIDKeyLen {
		return "", errJSONRPCNumericIDTooLarge
	}

	if len(digits) > scale {
		intPart := digits[:len(digits)-scale]
		fracPart := digits[len(digits)-scale:]
		if len(intPart)+1+len(fracPart) > maxCanonicalJSONRPCIDKeyLen {
			return "", errJSONRPCNumericIDTooLarge
		}
		result := intPart + "." + fracPart
		if sign != "" {
			result = sign + result
		}
		return result, nil
	}

	leadingZeros := scale - len(digits)
	if leadingZeros > maxCanonicalJSONRPCIDKeyLen-len(digits)-2 {
		return "", errJSONRPCNumericIDTooLarge
	}
	result := "0." + strings.Repeat("0", leadingZeros) + digits
	if sign != "" {
		result = sign + result
	}
	return result, nil
}

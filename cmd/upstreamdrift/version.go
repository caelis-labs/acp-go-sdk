package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type version struct {
	major int
	minor int
	patch int
	pre   []preIdent
	raw   string
}

type preIdent struct {
	numeric bool
	n       int
	s       string
}

func parseTag(tag string) (version, error) {
	original := strings.TrimSpace(tag)
	if original == "" {
		return version{}, fmt.Errorf("empty version tag")
	}
	rest := strings.TrimPrefix(original, "schema-")
	rest = strings.TrimPrefix(rest, "v")
	core, pre, hasPre := strings.Cut(rest, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("unsupported version tag %q", original)
	}
	major, err := parseVersionNumber(parts[0])
	if err != nil {
		return version{}, fmt.Errorf("unsupported version tag %q", original)
	}
	minor, err := parseVersionNumber(parts[1])
	if err != nil {
		return version{}, fmt.Errorf("unsupported version tag %q", original)
	}
	patch, err := parseVersionNumber(parts[2])
	if err != nil {
		return version{}, fmt.Errorf("unsupported version tag %q", original)
	}
	parsed := version{major: major, minor: minor, patch: patch, raw: original}
	if !hasPre {
		return parsed, nil
	}
	if pre == "" {
		return version{}, fmt.Errorf("unsupported version tag %q", original)
	}
	for _, ident := range strings.Split(pre, ".") {
		item, err := parsePreIdent(ident)
		if err != nil {
			return version{}, fmt.Errorf("unsupported version tag %q", original)
		}
		parsed.pre = append(parsed.pre, item)
	}
	return parsed, nil
}

func parseVersionNumber(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("empty number")
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return 0, fmt.Errorf("not a number")
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("negative number")
	}
	return n, nil
}

func parsePreIdent(value string) (preIdent, error) {
	if value == "" {
		return preIdent{}, fmt.Errorf("empty prerelease identifier")
	}
	numeric := true
	for _, r := range value {
		if unicode.IsDigit(r) {
			continue
		}
		if unicode.IsLetter(r) || r == '-' {
			numeric = false
			continue
		}
		return preIdent{}, fmt.Errorf("invalid prerelease identifier")
	}
	if numeric {
		n, err := strconv.Atoi(value)
		if err != nil {
			return preIdent{}, err
		}
		return preIdent{numeric: true, n: n, s: value}, nil
	}
	return preIdent{s: value}, nil
}

func (v version) Compare(other version) int {
	if v.major != other.major {
		return compareInt(v.major, other.major)
	}
	if v.minor != other.minor {
		return compareInt(v.minor, other.minor)
	}
	if v.patch != other.patch {
		return compareInt(v.patch, other.patch)
	}
	return comparePre(v.pre, other.pre)
}

func comparePre(a, b []preIdent) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if c := comparePreIdent(a[i], b[i]); c != 0 {
			return c
		}
	}
	return compareInt(len(a), len(b))
}

func comparePreIdent(a, b preIdent) int {
	if a.numeric != b.numeric {
		if a.numeric {
			return -1
		}
		return 1
	}
	if a.numeric {
		return compareInt(a.n, b.n)
	}
	return strings.Compare(a.s, b.s)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func latestTag(tags []string, include func(version) bool) (string, error) {
	var best version
	found := false
	for _, tag := range tags {
		parsed, err := parseTag(tag)
		if err != nil {
			continue
		}
		if include != nil && !include(parsed) {
			continue
		}
		if !found || parsed.Compare(best) > 0 {
			best = parsed
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("no matching version tags")
	}
	return best.raw, nil
}

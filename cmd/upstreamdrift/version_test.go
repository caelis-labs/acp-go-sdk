package main

import (
	"strings"
	"testing"
)

func TestParseAndCompareTags(t *testing.T) {
	t.Parallel()

	less := [][2]string{
		{"schema-v1.9.0", "schema-v1.21.0"},
		{"v2.0.0", "v2.1.0"},
		{"schema-v2.0.0-alpha.2", "schema-v2.0.0-alpha.3"},
		{"schema-v2.0.0-alpha.3", "schema-v2.0.0"},
		{"v1.4.0-rc.1", "v1.4.0"},
	}
	for _, pair := range less {
		left, err := parseTag(pair[0])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[0], err)
		}
		right, err := parseTag(pair[1])
		if err != nil {
			t.Fatalf("parse %q: %v", pair[1], err)
		}
		if got := left.Compare(right); got >= 0 {
			t.Fatalf("%q compared to %q = %d, want negative", pair[0], pair[1], got)
		}
		if got := right.Compare(left); got <= 0 {
			t.Fatalf("%q compared to %q = %d, want positive", pair[1], pair[0], got)
		}
	}

	same, err := parseTag("v2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	other, err := parseTag("v2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if same.Compare(other) != 0 {
		t.Fatal("identical tags should compare equal")
	}
}

func TestLatestTagSelectsSemverNotLexicographic(t *testing.T) {
	t.Parallel()

	got, err := latestTag([]string{"schema-v1.9.0", "schema-v1.21.0", "schema-v1.20.0"}, func(v version) bool {
		return len(v.pre) == 0
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "schema-v1.21.0" {
		t.Fatalf("latest = %q, want schema-v1.21.0", got)
	}

	got, err = latestTag([]string{"schema-v2.0.0-alpha.1", "schema-v2.0.0-alpha.3", "schema-v1.21.0"}, func(v version) bool {
		return strings.HasPrefix(v.raw, "schema-v2.")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != "schema-v2.0.0-alpha.3" {
		t.Fatalf("latest v2 = %q, want schema-v2.0.0-alpha.3", got)
	}
}

func TestParseTagRejectsMalformed(t *testing.T) {
	t.Parallel()
	for _, tag := range []string{"", "main", "1", "v1.2", "schema-v1.21.0-", "v1.21.0+build"} {
		if _, err := parseTag(tag); err == nil {
			t.Fatalf("parseTag(%q) succeeded, want error", tag)
		}
	}
}

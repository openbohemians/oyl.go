package oyl

import (
	"strings"
	"testing"
)

func TestUpgradePyYAML(t *testing.T) {
	in := "%YAML 1.1\n---\non: push  # mine\nenabled: yes\nmode: 0755\nratio: 1e3\n"
	out, fixes, err := Upgrade([]byte(in), WithDialect(YAML11))
	if err != nil {
		t.Fatal(err)
	}
	want := "---\n\"on\": push  # mine\nenabled: true\nmode: !!int 0o755\nratio: \"1e3\"\n"
	if string(out) != want {
		t.Fatalf("got %q, want %q", out, want)
	}
	if len(fixes) != 5 || fixes[0].Kind != FixDirective || fixes[1].Kind != FixScalar {
		t.Fatalf("fixes: %+v", fixes)
	}
	on := fixes[1]
	if on.Line != 3 || on.Column != 1 || on.Before != "on" || on.After != `"on"` || !on.Fixed ||
		!strings.Contains(on.Message, "the text kept") {
		t.Fatalf("the on key's fix: %+v", on)
	}
}

func TestUpgradeGoYAML2(t *testing.T) {
	out, _, err := Upgrade([]byte("a: y\nb: 2001-12-14\n"), WithDialect(GoYAML2))
	if err != nil || string(out) != "a: true\nb: 2001-12-14\n" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestUpgradeReaderUnknown(t *testing.T) {
	out, fixes, err := Upgrade([]byte("a: yes\nb: 1e3\n"))
	if err != nil || string(out) != "a: true\nb: 1e3\n" {
		t.Fatalf("got %q, %v", out, err)
	}
	if len(fixes) != 2 || fixes[1].Fixed || fixes[1].After != "" ||
		!strings.Contains(fixes[1].Message, "name the dialect") {
		t.Fatalf("fixes: %+v", fixes)
	}
}

func TestUpgradePolicy(t *testing.T) {
	out, _, err := Upgrade([]byte("on: 1\na: yes\n"), WithDialect(YAML11), UpgradeKeysKeepValue(),
		UpgradeValuesKeepText())
	if err != nil || string(out) != "true: 1\na: \"yes\"\n" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestUpgradeError(t *testing.T) {
	_, _, err := Upgrade([]byte("a: [1\n"), WithDialect(YAML11))
	if e, ok := err.(*Error); !ok || e.Line == 0 {
		t.Fatalf("got %v", err)
	}
}

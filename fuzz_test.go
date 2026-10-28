package oyl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// Whatever the input, YAMLToJSON returns valid JSON or an error, and never
// crashes.
func FuzzYAMLToJSON(f *testing.F) {
	paths, _ := filepath.Glob("testdata/*.yaml")
	for _, p := range paths {
		if in, err := os.ReadFile(p); err == nil {
			f.Add(in)
		}
	}
	f.Fuzz(func(t *testing.T, in []byte) {
		out, err := YAMLToJSON(in)
		if err == nil && !json.Valid(out) {
			t.Fatalf("invalid JSON %q for %q", out, in)
		}
	})
}

// Whatever the input, Unmarshal returns data or an error, and never
// crashes; data Marshal writes reads back the same, by every dialect.
func FuzzRoundTrip(f *testing.F) {
	paths, _ := filepath.Glob("testdata/*.yaml")
	for _, p := range paths {
		if in, err := os.ReadFile(p); err == nil {
			f.Add(in, uint8(0))
		}
	}
	f.Fuzz(func(t *testing.T, in []byte, dialect uint8) {
		d := Dialect(dialect % 3)
		var v any
		if Unmarshal(in, &v, WithDialect(d)) != nil {
			return
		}
		out, err := Marshal(v)
		if err != nil {
			t.Fatalf("%q read by %v as %#v, which Marshal can't write: %v", in, d, v, err)
		}
		for _, d := range []Dialect{YAML12, YAML11, GoYAML2} {
			var back any
			if err := Unmarshal(out, &back, WithDialect(d)); err != nil {
				t.Fatalf("%#v wrote %q, which %v can't read: %v", v, out, d, err)
			}
			if !sameData(back, v) {
				t.Fatalf("%#v wrote %q, read by %v as %#v", v, out, d, back)
			}
		}
	})
}

// sameData is reflect.DeepEqual, but NaN equals NaN and times compare as
// instants.
func sameData(a, b any) bool {
	switch a := a.(type) {
	case float64:
		b, ok := b.(float64)
		return ok && (a == b || a != a && b != b)
	case time.Time:
		b, ok := b.(time.Time)
		return ok && a.Equal(b)
	case []any:
		b, ok := b.([]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !sameData(a[i], b[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		b, ok := b.(map[string]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, x := range a {
			if y, ok := b[k]; !ok || !sameData(x, y) {
				return false
			}
		}
		return true
	case map[any]any:
		b, ok := b.(map[any]any)
		if !ok || len(a) != len(b) {
			return false
		}
		for k, x := range a {
			if f, ok := k.(float64); ok && f != f {
				continue // a NaN key can't be looked up
			}
			if y, ok := b[k]; !ok || !sameData(x, y) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

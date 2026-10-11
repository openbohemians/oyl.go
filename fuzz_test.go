package oyl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
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

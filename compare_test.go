package oyl

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

// Every file in testdata converts byte for byte as sigs.k8s.io/yaml converts
// it, or both fail. More YAML can be compared with OYL_COMPARE=dir.
func TestSameAsSigsYAML(t *testing.T) {
	dirs := []string{"testdata"}
	if d := os.Getenv("OYL_COMPARE"); d != "" {
		dirs = append(dirs, d)
	}
	n := 0
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			if ext := filepath.Ext(path); ext != ".yaml" && ext != ".yml" {
				return nil
			}
			in, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			n++
			compare(t, path, in, YAMLToJSON, sigsyaml.YAMLToJSON)
			compare(t, path+" (strict)", in, YAMLToJSONStrict, sigsyaml.YAMLToJSONStrict)
			return nil
		})
	}
	t.Logf("%d files", n)
	if n < 10 {
		t.Errorf("only %d files compared", n)
	}
}

func compare(t *testing.T, name string, in []byte, ours, theirs func([]byte) ([]byte, error)) {
	t.Helper()
	got, err1 := ours(in)
	want, err2 := theirs(in)
	switch {
	case err1 != nil && err2 != nil:
	case err1 != nil:
		t.Errorf("%s: oyl failed (%v), sigs.k8s.io/yaml gave %s", name, err1, want)
	case err2 != nil:
		t.Errorf("%s: sigs.k8s.io/yaml failed (%v), oyl gave %s", name, err2, got)
	case !bytes.Equal(got, want):
		t.Errorf("%s:\n oyl  %s\n sigs %s", name, got, want)
	}
}

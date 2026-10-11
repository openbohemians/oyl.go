package oyl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

// A large CRD-like document, as the big manifests in real clusters are
func bigDocument() []byte {
	var b strings.Builder
	b.WriteString("apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: big.example.com\nspec:\n  properties:\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&b, "    field%d:\n      type: object\n      description: |\n        Field %d holds a value, with a long description that spans\n        two lines of text.\n      properties:\n        size: {type: integer, minimum: 1, maximum: %d}\n        mode: {type: string, enum: [fast, safe, \"off\"]}\n        ratio: {type: number, default: 0.5}\n", i, i, i*1000+7)
	}
	return []byte(b.String())
}

func testdataDocuments(b *testing.B) [][]byte {
	var docs [][]byte
	paths, _ := filepath.Glob("testdata/*.yaml")
	for _, p := range paths {
		if strings.Contains(p, "error") {
			continue
		}
		in, err := os.ReadFile(p)
		if err != nil {
			b.Fatal(err)
		}
		docs = append(docs, in)
	}
	return docs
}

func benchmark(b *testing.B, docs [][]byte, convert func([]byte) ([]byte, error)) {
	size := 0
	for _, d := range docs {
		size += len(d)
	}
	b.SetBytes(int64(size))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, d := range docs {
			convert(d)
		}
	}
}

// The YAML files under OYL_BENCH, to measure your own manifests
func dirDocuments(b *testing.B) [][]byte {
	dir := os.Getenv("OYL_BENCH")
	if dir == "" {
		b.Skip("set OYL_BENCH to a directory of YAML files")
	}
	var docs [][]byte
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml") {
			if in, err := os.ReadFile(path); err == nil {
				docs = append(docs, in)
			}
		}
		return nil
	})
	return docs
}

func BenchmarkOylDir(b *testing.B)  { benchmark(b, dirDocuments(b), YAMLToJSON) }
func BenchmarkSigsDir(b *testing.B) { benchmark(b, dirDocuments(b), sigsyaml.YAMLToJSON) }

func BenchmarkOylBig(b *testing.B)  { benchmark(b, [][]byte{bigDocument()}, YAMLToJSON) }
func BenchmarkSigsBig(b *testing.B) { benchmark(b, [][]byte{bigDocument()}, sigsyaml.YAMLToJSON) }
func BenchmarkOylManifests(b *testing.B) {
	benchmark(b, testdataDocuments(b), YAMLToJSON)
}
func BenchmarkSigsManifests(b *testing.B) {
	benchmark(b, testdataDocuments(b), sigsyaml.YAMLToJSON)
}

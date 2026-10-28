package oyl

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
	yamlv3 "sigs.k8s.io/yaml/goyaml.v3"
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

// Decoding into interface values, and writing them back: Oyl against
// go-yaml v3 and sigs.k8s.io/yaml (go-yaml v2, then encoding/json).

func benchmarkDecode(b *testing.B, docs [][]byte, unmarshal func([]byte, any) error) {
	benchmark(b, docs, func(in []byte) ([]byte, error) {
		var v any
		return nil, unmarshal(in, &v)
	})
}

func benchmarkEncode(b *testing.B, docs [][]byte, marshal func(any) ([]byte, error)) {
	var values []any
	var kept [][]byte
	for _, d := range docs {
		var v any
		if Unmarshal(d, &v, WithDialect(GoYAML2)) == nil {
			values = append(values, v)
			kept = append(kept, d)
		}
	}
	i := 0
	benchmark(b, kept, func([]byte) ([]byte, error) {
		v := values[i%len(values)]
		i++
		return marshal(v)
	})
}

func oylUnmarshal(in []byte, v any) error { return Unmarshal(in, v) }

func BenchmarkDecodeOylBig(b *testing.B) { benchmarkDecode(b, [][]byte{bigDocument()}, oylUnmarshal) }
func BenchmarkDecodeV3Big(b *testing.B) {
	benchmarkDecode(b, [][]byte{bigDocument()}, yamlv3.Unmarshal)
}
func BenchmarkDecodeSigsBig(b *testing.B) {
	benchmarkDecode(b, [][]byte{bigDocument()}, func(in []byte, v any) error { return sigsyaml.Unmarshal(in, v) })
}
func BenchmarkDecodeOylManifests(b *testing.B) {
	benchmarkDecode(b, testdataDocuments(b), oylUnmarshal)
}
func BenchmarkDecodeV3Manifests(b *testing.B) {
	benchmarkDecode(b, testdataDocuments(b), yamlv3.Unmarshal)
}
func BenchmarkDecodeOylDir(b *testing.B) { benchmarkDecode(b, dirDocuments(b), oylUnmarshal) }
func BenchmarkDecodeV3Dir(b *testing.B)  { benchmarkDecode(b, dirDocuments(b), yamlv3.Unmarshal) }
func BenchmarkDecodeSigsDir(b *testing.B) {
	benchmarkDecode(b, dirDocuments(b), func(in []byte, v any) error { return sigsyaml.Unmarshal(in, v) })
}

func BenchmarkEncodeOylBig(b *testing.B) { benchmarkEncode(b, [][]byte{bigDocument()}, Marshal) }
func BenchmarkEncodeV3Big(b *testing.B)  { benchmarkEncode(b, [][]byte{bigDocument()}, yamlv3.Marshal) }
func BenchmarkEncodeSigsBig(b *testing.B) {
	benchmarkEncode(b, [][]byte{bigDocument()}, sigsyaml.Marshal)
}
func BenchmarkEncodeOylDir(b *testing.B) { benchmarkEncode(b, dirDocuments(b), Marshal) }
func BenchmarkEncodeV3Dir(b *testing.B)  { benchmarkEncode(b, dirDocuments(b), yamlv3.Marshal) }

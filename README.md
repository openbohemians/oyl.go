# oyl.go

Go bindings for [Oyl](https://github.com/openbohemians/oyl), the optimized
YAML library: YAML to JSON the way Kubernetes reads it, about five to six
times as fast.

```go
import "github.com/openbohemians/oyl.go"

json, err := oyl.YAMLToJSON(manifest)
```

`oyl.YAMLToJSON` and `oyl.YAMLToJSONStrict` give byte for byte the JSON of
[sigs.k8s.io/yaml](https://github.com/kubernetes-sigs/yaml)'s functions of
the same names, the ones behind kubectl and client-go's YAML decoding:
go-yaml v2's types (`yes` and `on` are true, `0777` is octal, `1e3` is a
float), its merge-key order, the first document only, keys sorted, and
numbers and strings written as Go's `encoding/json` writes them. They can
replace `sigs.k8s.io/yaml`'s anywhere it converts YAML to JSON.

**Status:** this needs Oyl 1.1 (its dialects and `oyl_parse_json`), not
yet released; until then it vendors Oyl's development branch, and
`c/COMMIT` says which commit. Tags of this module start with Oyl 1.1.

Oyl does the conversion in C, in one call per document. Its sources are
vendored in `c/`, so building needs only a C compiler: cgo must be on
(`CGO_ENABLED=1`, the default when not cross-compiling).

## Same output, checked

`go test` converts every file in `testdata/` with both libraries and
compares the bytes (or that both fail). Point `OYL_COMPARE` at a directory
to compare your own YAML:

```sh
OYL_COMPARE=path/to/manifests go test -run TestSameAsSigsYAML -v
```

It was run on 866 documents of public Kubernetes manifests (the
Kubernetes examples and website, and the Argo CD, cert-manager and
Prometheus Operator install bundles): all identical, or failing in both.

Where they can differ:

- **Nesting deeper than 256 levels** is an error here (`MaxDepth`); go-yaml
  v2 allows 10,000. Oyl's parser recurses on some input, and cgo runs it
  on threads whose stacks may be small.
- **Error messages** differ. As with `sigs.k8s.io/yaml`, they carry no
  compatibility promise; Oyl's give the line (`yaml: line 3: ...`) and
  are an `*oyl.Error` with the column too.
- **Some of libyaml's syntax quirks** aren't copied: `?x` as an explicit
  key, anchor names cut at `:`, and text after the first document's root
  node ignored even when malformed. Each is malformed or rare YAML.

## Speed

```sh
go test -run '^$' -bench .
```

| Input | oyl | sigs.k8s.io/yaml |
|---|---|---|
| small manifests (`testdata/`) | 69 MB/s | 11.5 MB/s |
| a 1 MB CRD-like document | 94 MB/s | 18 MB/s |

(Intel Core Ultra 7 155H, Go 1.27.)

## Updating Oyl

`c/` holds a copy of Oyl's sources, and `c/COMMIT` names the commit they
came from. From a checkout of Oyl:

```sh
scripts/sync-oyl.sh path/to/oyl          # its committed HEAD
scripts/sync-oyl.sh path/to/oyl v1.1.0   # or a release tag
go test
```

The script also writes one small `.c` file per Oyl source in the package
directory, which includes it: cgo compiles only the package's own
directory.

## License

MIT; Oyl's own license is in `c/LICENSE`.

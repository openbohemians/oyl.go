# oyl.go

Go bindings for [Oyl](https://github.com/openbohemians/oyl), the optimized
YAML library: decoding YAML into Go values and writing them as YAML, with
go-yaml's API, and YAML to JSON the way Kubernetes reads it. Four to
seven times as fast as go-yaml and sigs.k8s.io/yaml on real manifests.

```go
import "github.com/openbohemians/oyl.go"

var cfg Config
err := oyl.Unmarshal(data, &cfg)  // YAML 1.2
out, err := oyl.Marshal(cfg)

json, err := oyl.YAMLToJSON(manifest)  // as Kubernetes reads YAML
```

**Status:** it vendors Oyl 1.2.0; `c/COMMIT` names the commit. Oyl's work
is done in C, one call per document. Its sources are vendored in `c/`, so
building needs only a C compiler: cgo must be on (`CGO_ENABLED=1`, the
default when not cross-compiling).

## Decoding and encoding

`Unmarshal`, `Marshal`, `NewDecoder` and `NewEncoder` work as go-yaml's
do:

- A struct's keys are its fields' `yaml` tags, else their `json` tags (so
  Kubernetes types work, `json:",inline"` included), else the field names
  lowercased; with `omitempty`, `flow`, `inline` and `-`.
- Decoding into an interface gives `map[string]any` (or `map[any]any`
  when a key isn't a string), `[]any`, `int` (else `int64` or `uint64`),
  `float64`, `bool`, `string` and `nil`. A string field takes any
  scalar's text, and null sets the zero value.
- Aliases are followed and `<<` merge keys applied. A key written twice in
  a mapping is an error, as YAML says. Alias bombs fail instead of
  exhausting memory.
- `time.Time` reads timestamps and is written `!!timestamp`;
  `time.Duration` reads and writes Go durations (`1m30s`); `[]byte` reads
  and writes `!!binary`. Types implementing `Marshaler`, `Unmarshaler`,
  `encoding.TextMarshaler` or `encoding.TextUnmarshaler` handle
  themselves.
- `WithKnownFields()` (or `Decoder.KnownFields`) makes a key no field
  matches an error. A `Decoder` reads a stream's documents one by one,
  then gives `io.EOF`.

Scalars are typed by YAML 1.2 by default: `yes`, `on` and dates are
strings, `0777` is 777. `WithDialect` reads older YAML:

```go
var v map[string]any
oyl.Unmarshal([]byte("enabled: yes\nmode: 0755\n"), &v, oyl.WithDialect(oyl.YAML11))
// map[enabled:true mode:493]
```

| Dialect | Types |
|---------|-------|
| `YAML12` | YAML 1.2's Core schema (the default) |
| `YAML11` | YAML 1.1 as PyYAML reads it: also `yes`/`no`/`on`/`off`, 0-octal, `0b`, base 60, timestamps |
| `GoYAML2` | go-yaml v2's, as Kubernetes reads YAML: YAML 1.1's and `y`/`n`; its merge order |

A document that starts with `%YAML 1.1` is read as YAML 1.1 whatever the
dialect.

What `Marshal` writes reads back as the same data in YAML 1.2, YAML 1.1
and go-yaml v2 readers alike: a string any of them would read as another
type is quoted (`"yes"`, `"on"`, `"y"`, `"017"`, `"1:30"`, `""`), text with
line breaks is a literal block (`|`), and a float always has a dot (`1.0`,
`1.0e+20`). Map keys are sorted; nil slices and maps are `[]` and `{}`.
Values nested deeper than `MaxDepth` are an error, which catches cycles.

Where it differs from go-yaml v3: there is no `yaml.Node`, so
`Unmarshaler` takes go-yaml v2's form, `UnmarshalYAML(func(any) error)
error`; decoding stops at the first error, an `*oyl.Error` with the line
and column; integers beyond 64 bits are strings.

## YAML to JSON

`oyl.YAMLToJSON` and `oyl.YAMLToJSONStrict` give byte for byte the JSON of
[sigs.k8s.io/yaml](https://github.com/kubernetes-sigs/yaml)'s functions of
the same names, the ones behind kubectl and client-go's YAML decoding:
go-yaml v2's types (`yes` and `on` are true, `0777` is octal, `1e3` is a
float), its merge-key order, the first document only, keys sorted, and
numbers and strings written as Go's `encoding/json` writes them. They can
replace `sigs.k8s.io/yaml`'s anywhere it converts YAML to JSON.

### Same output, checked

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
OYL_BENCH=path/to/manifests go test -run '^$' -bench Dir   # your own YAML
```

YAML to JSON:

| Input | oyl | sigs.k8s.io/yaml | |
|---|---|---|---|
| 866 public Kubernetes documents, 7.3 MB (the comparison set above) | 333 MB/s | 48 MB/s | 7.0x |
| small manifests (`testdata/`, a few hundred bytes each) | 81 MB/s | 13.4 MB/s | 6.1x |
| a synthetic 1 MB CRD, all short keys | 124 MB/s | 16 MB/s | 7.7x |

Small documents spend more of their time on the fixed cost of a call.

Decoding into interface values, and writing those values back, against
go-yaml v3 (`go.yaml.in/yaml/v3`) and sigs.k8s.io/yaml (go-yaml v2, then
`encoding/json`). Speeds count the bytes of YAML read, for writing too:

| Input | oyl | go-yaml v3 | sigs.k8s.io/yaml |
|---|---|---|---|
| decode, the 866 Kubernetes documents | 207 MB/s | 45 MB/s (4.6x) | 37 MB/s (5.6x) |
| decode, the synthetic CRD | 73 MB/s | 12.9 MB/s (5.7x) | 12.2 MB/s (6.0x) |
| encode, the 866 Kubernetes documents | 176 MB/s | 28 MB/s (6.3x) | |
| encode, the synthetic CRD | 58 MB/s | 10.4 MB/s (5.6x) | 7.2 MB/s (8.1x) |

(Intel Core Ultra 7 155H, one core, Go 1.27.)

## Updating Oyl

`c/` holds a copy of Oyl's sources, and `c/COMMIT` names the commit they
came from. From a checkout of Oyl:

```sh
scripts/sync-oyl.sh path/to/oyl          # its committed HEAD
scripts/sync-oyl.sh path/to/oyl v1.2.0   # or a release tag
go test
```

The script also writes one small `.c` file per Oyl source in the package
directory, which includes it: cgo compiles only the package's own
directory.

## License

MIT; Oyl's own license is in `c/LICENSE`.

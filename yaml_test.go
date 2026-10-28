package oyl

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type Container struct {
	Name     string            `yaml:"name"`
	Image    string            `yaml:"image,omitempty"`
	Ports    []int             `yaml:"ports,flow"`
	Env      map[string]string `yaml:"env,omitempty"`
	Replicas *int              `yaml:"replicas"`
	Debug    bool
	ignored  int
	Skip     string `yaml:"-"`
}

func TestUnmarshalStruct(t *testing.T) {
	in := "name: web\nimage: nginx:1.27\nports: [80, 443]\nenv: {A: \"1\", B: two}\nreplicas: 3\ndebug: true\nother: x\n"
	var c Container
	if err := Unmarshal([]byte(in), &c); err != nil {
		t.Fatal(err)
	}
	three := 3
	want := Container{Name: "web", Image: "nginx:1.27", Ports: []int{80, 443}, Env: map[string]string{"A": "1", "B": "two"}, Replicas: &three, Debug: true}
	if !reflect.DeepEqual(c, want) {
		t.Errorf("got %+v\nwant %+v", c, want)
	}
	err := Unmarshal([]byte(in), &c, WithKnownFields())
	var e *Error
	if !errors.As(err, &e) || e.Line != 7 || !strings.Contains(e.Msg, "field other not found") {
		t.Errorf("known fields: got %v", err)
	}
}

// Kubernetes types have json tags, and inline their TypeMeta.
type TypeMeta struct {
	Kind       string `json:"kind,omitempty"`
	APIVersion string `json:"apiVersion,omitempty"`
}

type ConfigMap struct {
	TypeMeta `json:",inline"`
	Data     map[string]string `json:"data,omitempty"`
}

func TestJSONTags(t *testing.T) {
	in := "apiVersion: v1\nkind: ConfigMap\ndata:\n  enabled: \"yes\"\n  mode: '0644'\n"
	var cm ConfigMap
	if err := Unmarshal([]byte(in), &cm); err != nil {
		t.Fatal(err)
	}
	want := ConfigMap{TypeMeta{"ConfigMap", "v1"}, map[string]string{"enabled": "yes", "mode": "0644"}}
	if !reflect.DeepEqual(cm, want) {
		t.Errorf("got %+v", cm)
	}
	out, err := Marshal(cm)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "kind: ConfigMap\napiVersion: v1\ndata:\n  enabled: \"yes\"\n  mode: \"0644\"\n" {
		t.Errorf("got %q", out)
	}
}

func TestUnmarshalAny(t *testing.T) {
	in := "a: [1, -2, 9223372036854775808, 1.5, .inf, true, ~, yes, 2001-12-14]\n1: one\n"
	var v any
	if err := Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	want := map[any]any{
		"a": []any{1, -2, uint64(9223372036854775808), 1.5, math.Inf(1), true, nil, "yes", "2001-12-14"},
		1:   "one",
	}
	if !reflect.DeepEqual(v, want) {
		t.Errorf("got %#v", v)
	}
	if err := Unmarshal([]byte("a: 1\n"), &v); err != nil || !reflect.DeepEqual(v, map[string]any{"a": 1}) {
		t.Errorf("string keys: got %#v, %v", v, err)
	}
}

func TestDialects(t *testing.T) {
	in := []byte("[yes, on, y, 0777, 1:30, 2001-12-14]")
	cases := []struct {
		d    Dialect
		want []any
	}{
		{YAML12, []any{"yes", "on", "y", 777, "1:30", "2001-12-14"}},
		{YAML11, []any{true, true, "y", 511, 90, time.Date(2001, 12, 14, 0, 0, 0, 0, time.UTC)}},
		{GoYAML2, []any{true, true, true, 511, "1:30", "2001-12-14"}},
	}
	for _, c := range cases {
		var v []any
		if err := Unmarshal(in, &v, WithDialect(c.d)); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(v, c.want) {
			t.Errorf("%v: got %#v", c.d, v)
		}
	}
	var v map[string]any
	if err := Unmarshal([]byte("%YAML 1.1\n---\nenabled: yes\n"), &v); err != nil || v["enabled"] != true {
		t.Errorf("%%YAML 1.1: got %v, %v", v, err)
	}
}

func TestMerges(t *testing.T) {
	in := []byte("b: &b {a: 1, c: 2}\nx: {a: 9, <<: *b}\n")
	for _, c := range []struct {
		d    Dialect
		want int
	}{{YAML12, 9}, {GoYAML2, 1}} {
		var v map[string]map[string]int
		if err := Unmarshal(in, &v, WithDialect(c.d)); err != nil {
			t.Fatal(err)
		}
		if v["x"]["a"] != c.want || v["x"]["c"] != 2 {
			t.Errorf("%v: got %v", c.d, v["x"])
		}
	}
}

func TestUnmarshalErrors(t *testing.T) {
	cases := []struct {
		in   string
		into any
		line int
		msg  string
	}{
		{"a: 1\na: 2\n", new(map[string]int), 2, `mapping key "a" already defined`},
		{"a: [1, x]\n", new(map[string][]int), 1, "cannot unmarshal !!str `x` into int"},
		{"a: 300\n", new(map[string]int8), 1, "cannot unmarshal !!int `300` into int8"},
		{"a: !!int x\n", new(map[string]any), 1, "invalid !!int `x`"},
		{"a: {b: 1}\n", new(map[string]string), 1, "cannot unmarshal !!map into string"},
		{"a: [1, 2\n", new(any), 2, "expected"},
		{"- &a [*a]\n", new(any), 1, ""},
	}
	for _, c := range cases {
		err := Unmarshal([]byte(c.in), c.into)
		var e *Error
		if !errors.As(err, &e) || (c.line > 0 && e.Line != c.line) || !strings.Contains(e.Msg, c.msg) {
			t.Errorf("%q: got %v", c.in, err)
		}
	}
	if err := Unmarshal([]byte("a: 1"), map[string]int{}); err == nil {
		t.Error("want an error for a non-pointer")
	}
	// an alias bomb fails instead of exhausting memory
	bomb := "a: &a [x, x, x, x, x, x, x, x, x, x]\n"
	for i := 'b'; i <= 'k'; i++ {
		p := string(i - 1)
		bomb += string(i) + ": &" + string(i) + " [*" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + ", *" + p + "]\n"
	}
	var v any
	if err := Unmarshal([]byte(bomb), &v); err == nil || !strings.Contains(err.Error(), "aliases") {
		t.Errorf("alias bomb: got %v", err)
	}
}

type Celsius float64

func (c *Celsius) UnmarshalYAML(unmarshal func(any) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(s, "C"), 64)
	if err != nil || !strings.HasSuffix(s, "C") {
		return errors.New("want degrees C")
	}
	*c = Celsius(n)
	return nil
}

func (c Celsius) MarshalYAML() (any, error) {
	return formatFloat(float64(c), 64) + "C", nil
}

func TestSpecialTypes(t *testing.T) {
	var v struct {
		When    time.Time
		Wait    time.Duration
		Data    []byte
		Text    string
		IP      net.IP
		Temp    Celsius
		Temps   []Celsius
		Pointer *Celsius
	}
	in := "when: 2001-12-14T21:59:43.1-05:00\nwait: 1m30s\ndata: !!binary aGk=\ntext: !!binary aGk=\nip: 10.0.0.1\ntemp: 21.5C\ntemps: [1C, 2C]\npointer: 3C\n"
	if err := Unmarshal([]byte(in), &v); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2001, 12, 14, 21, 59, 43, 100_000_000, time.FixedZone("", -5*3600))
	if !v.When.Equal(when) || v.Wait != 90*time.Second || string(v.Data) != "hi" || v.Text != "hi" ||
		!v.IP.Equal(net.IPv4(10, 0, 0, 1)) || v.Temp != 21.5 || !reflect.DeepEqual(v.Temps, []Celsius{1, 2}) || *v.Pointer != 3 {
		t.Errorf("got %+v", v)
	}
	out, err := Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "when: !!timestamp 2001-12-14T21:59:43.1-05:00\nwait: 1m30s\ndata: !!binary aGk=\ntext: hi\nip: 10.0.0.1\ntemp: 21.5C\ntemps:\n  - 1.0C\n  - 2.0C\npointer: 3.0C\n"
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

func TestDecoder(t *testing.T) {
	dec := NewDecoder(strings.NewReader("a: 1\n---\n- x\n---\n"), WithDialect(YAML11))
	var docs []any
	for {
		var v any
		err := dec.Decode(&v)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, v)
	}
	if !reflect.DeepEqual(docs, []any{map[string]any{"a": 1}, []any{"x"}, nil}) {
		t.Errorf("got %#v", docs)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		t.Errorf("after the end: got %v", err)
	}
	var v any
	if err := Unmarshal(nil, &v); err != nil || v != nil {
		t.Errorf("no input: got %v, %v", v, err)
	}
}

func TestMarshal(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{[]string{"yes", "on", "y", "017", "1:30", "", "~", "2001-12-14", "<<", "plain"}, "- \"yes\"\n- \"on\"\n- \"y\"\n- \"017\"\n- \"1:30\"\n- \"\"\n- \"~\"\n- \"2001-12-14\"\n- \"<<\"\n- plain\n"},
		{[]float64{1, 1e20, -2.5e-7, 0.1, math.Inf(-1), math.NaN()}, "- 1.0\n- 1.0e+20\n- -2.5e-07\n- 0.1\n- -.inf\n- .nan\n"},
		{map[any]any{"b": 1, 2: true, "a": nil, 1.5: "x", false: []int(nil)}, "false: []\n1.5: x\n2: true\na: null\nb: 1\n"},
		{map[string]any{"text": "one\ntwo\n"}, "text: |\n  one\n  two\n"},
		{Container{Name: "web", Ports: []int{80}}, "name: web\nports: [80]\nreplicas: null\ndebug: false\n"},
		{nil, "null\n"},
		{uint64(math.MaxUint64), "18446744073709551615\n"},
		{[2]bool{true, false}, "- true\n- false\n"},
	}
	for _, c := range cases {
		got, err := Marshal(c.in)
		if err != nil {
			t.Errorf("%#v: %v", c.in, err)
		} else if string(got) != c.want {
			t.Errorf("%#v:\n got  %q\n want %q", c.in, got, c.want)
		}
	}
	type cycle struct{ Next *cycle }
	loop := &cycle{}
	loop.Next = loop
	if _, err := Marshal(loop); err == nil {
		t.Error("a cycle: want an error")
	}
	if _, err := Marshal(make(chan int)); err == nil {
		t.Error("a channel: want an error")
	}
}

func TestEncoder(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	enc.SetIndent(4)
	for _, v := range []any{1, "two", map[string][]int{"a": {3}}} {
		if err := enc.Encode(v); err != nil {
			t.Fatal(err)
		}
	}
	enc.Close()
	if buf.String() != "1\n---\ntwo\n---\na:\n    - 3\n" {
		t.Errorf("got %q", buf.String())
	}
}

// Decoding as go-yaml v2 gives the data YAMLToJSON writes.
func TestUnmarshalAgreesWithYAMLToJSON(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.yaml")
	n := 0
	for _, path := range files {
		in, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := YAMLToJSON(in)
		if err != nil {
			continue
		}
		var v any
		if err := Unmarshal(in, &v, WithDialect(GoYAML2)); err != nil {
			continue // a repeated key: an error here, the last value there
		}
		got, err := json.Marshal(jsonKeys(v))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		n++
		if !bytes.Equal(got, want) {
			t.Errorf("%s:\n got  %s\n want %s", path, got, want)
		}
	}
	if n < 5 {
		t.Errorf("only %d files compared", n)
	}
}

// jsonKeys makes map keys strings, as sigs.k8s.io/yaml does (a float with
// 32-bit precision).
func jsonKeys(v any) any {
	switch v := v.(type) {
	case []any:
		for i := range v {
			v[i] = jsonKeys(v[i])
		}
	case map[string]any:
		for k := range v {
			v[k] = jsonKeys(v[k])
		}
	case map[any]any:
		m := map[string]any{}
		for k, x := range v {
			var s string
			switch k := k.(type) {
			case string:
				s = k
			case int:
				s = strconv.Itoa(k)
			case uint64:
				s = strconv.FormatUint(k, 10)
			case float64:
				s = strconv.FormatFloat(k, 'g', -1, 32)
			case bool:
				s = strconv.FormatBool(k)
			}
			m[s] = jsonKeys(x)
		}
		return m
	}
	return v
}

// Random values read back the same, by every dialect.

var pieces = []string{"a", "b", "yes", "no", "on", "y", "N", "true", "null", "~", "0", "017", "0x1F", "0o7",
	"0b101", "1:30", "1_000", "2001-12-14", ".5", "-.5", "1e3", ".inf", "-", ":", "#", " ", "  ", "\t", "\n", "\r",
	"\r\n", "'", "\"", "\\", "&a", "*a", "!x", "|", ">", "%", "@", "`", "{", "}", "[", "]", ",", "?", "é", "日本",
	"\u0085", "\u2028", "\x00", "\x7f", "\ufeff", "<<", "=", "- x", ": y", "? z", "...", "---"}

func randString(r *rand.Rand) string {
	var b strings.Builder
	for n := r.Intn(6); n > 0; n-- {
		b.WriteString(pieces[r.Intn(len(pieces))])
	}
	return b.String()
}

func randValue(r *rand.Rand, depth int) any {
	if depth >= 3 || r.Intn(3) > 0 {
		switch r.Intn(8) {
		case 0:
			return nil
		case 1:
			return r.Intn(2) == 1
		case 2:
			return int(r.Uint64())
		case 3:
			return r.Uint64() | 1<<63
		case 4:
			if f := math.Float64frombits(r.Uint64()); !math.IsNaN(f) {
				return f
			}
			return 0.25
		case 5:
			return []float64{math.Inf(1), math.Inf(-1), 0, 1, 1e300, 5e-324, 1e-7}[r.Intn(7)]
		}
		return randString(r)
	}
	if r.Intn(2) == 0 {
		s := make([]any, r.Intn(5))
		for i := range s {
			s[i] = randValue(r, depth+1)
		}
		return s
	}
	m := map[string]any{}
	for n := r.Intn(5); n > 0; n-- {
		m[randString(r)] = randValue(r, depth+1)
	}
	return m
}

func TestRandomRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 3000; i++ {
		v := randValue(r, 0)
		var buf bytes.Buffer
		enc := NewEncoder(&buf)
		enc.SetIndent(1 + r.Intn(10))
		if err := enc.Encode(v); err != nil {
			t.Fatalf("%#v: %v", v, err)
		}
		for _, d := range []Dialect{YAML12, YAML11, GoYAML2} {
			var back any
			if err := Unmarshal(buf.Bytes(), &back, WithDialect(d)); err != nil {
				t.Fatalf("%#v wrote %q, which %v can't read: %v", v, buf.String(), d, err)
			}
			if !reflect.DeepEqual(back, v) {
				t.Fatalf("%#v wrote %q, read by %v as %#v", v, buf.String(), d, back)
			}
		}
	}
}

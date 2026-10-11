package oyl

import (
	"errors"
	"testing"
)

func TestYAMLToJSON(t *testing.T) {
	cases := []struct{ in, want string }{
		{"apiVersion: v1\nkind: Pod\n", `{"apiVersion":"v1","kind":"Pod"}`},
		{"a: yes\nb: on\nc: 0777\nd: 1_000\ne: 08\n", `{"a":true,"b":true,"c":511,"d":1000,"e":8}`},
		{"z: 1\nB: 2\na: 3\n", `{"B":2,"a":3,"z":1}`},
		{"s: \"<b>&</b>\"\n", `{"s":"\u003cb\u003e\u0026\u003c/b\u003e"}`},
		{"f: [0.000001, 0.0000001, 1e21, 100.0]\n", `{"f":[0.000001,1e-7,1e+21,100]}`},
		{"d: &d {h: a}\ne:\n  h: b\n  <<: *d\n", `{"d":{"h":"a"},"e":{"h":"a"}}`},
		{"a: |\n  x", `{"a":"x"}`},
		{"q: \"it\\'s\"\n", `{"q":"it's"}`},
		{"a: 1\n---\nb: 2\n", `{"a":1}`},
		{"", `null`},
	}
	for _, c := range cases {
		got, err := YAMLToJSON([]byte(c.in))
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("%q:\n got  %s\n want %s", c.in, got, c.want)
		}
	}
}

func TestErrors(t *testing.T) {
	for _, in := range []string{"a: [1, 2\n", "~: x\n", "a: .inf\n", "%YAML 1.2\n---\na: b\n"} {
		_, err := YAMLToJSON([]byte(in))
		var e *Error
		if !errors.As(err, &e) {
			t.Errorf("%q: want an *Error, got %v", in, err)
		}
	}
	_, err := YAMLToJSON([]byte("a: 1\nb: [1, 2\n"))
	var e *Error
	if !errors.As(err, &e) || e.Line != 3 {
		t.Errorf("want an error at line 3, got %v", err)
	}
	if _, err := YAMLToJSONStrict([]byte("a: 1\na: 2\n")); err == nil {
		t.Error("YAMLToJSONStrict: want an error for a repeated key")
	}
}

func TestDepth(t *testing.T) {
	deep := make([]byte, 0, 2*MaxDepth+4)
	for i := 0; i <= MaxDepth; i++ {
		deep = append(deep, '[')
	}
	for i := 0; i <= MaxDepth; i++ {
		deep = append(deep, ']')
	}
	if _, err := YAMLToJSON(deep); err == nil {
		t.Errorf("nesting %d deep: want an error", MaxDepth+1)
	}
}

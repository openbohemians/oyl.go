// Package oyl converts YAML to JSON with Oyl, the optimized YAML library
// (https://github.com/openbohemians/oyl), the way Kubernetes reads YAML.
//
// YAMLToJSON gives byte for byte the JSON of sigs.k8s.io/yaml's YAMLToJSON,
// the function behind kubectl and client-go's YAML decoding: go-yaml v2's
// types (yes and on are true, 0777 is octal), its merge-key order, keys
// sorted, numbers and strings as Go's encoding/json writes them. Oyl does
// it in C, in one call per document, about seven times as fast on real
// manifests.
//
// Oyl's C sources are vendored, so building needs only a C compiler (cgo).
package oyl

/*
#cgo CFLAGS: -std=c11 -O2 -I${SRCDIR}/c/include
#cgo LDFLAGS: -lm
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "oyl/oyl.h"

typedef struct {
    char  *json;      // malloc'd, for Go to copy and free
    size_t len;
    int    status;
    size_t line, col;
    char   msg[320];
} oyl_go_result;

// The first document of yaml as JSON, as Kubernetes reads YAML: the go-yaml
// v2 dialect (types, merge order, libyaml's leniencies). An empty stream
// is null, as in sigs.k8s.io/yaml.
static void oyl_go_yaml_to_json(const char *yaml, size_t len, int strict, int max_depth,
                                oyl_go_result *r) {
    memset(r, 0, sizeof *r);
    oyl_arena *a = oyl_arena_new(len / 2 + 4096);
    oyl_parser *p = a ? oyl_parser_new(yaml ? yaml : "", len, a) : NULL;
    if (!p) {
        r->status = OYL_ERR_MEMORY;
        snprintf(r->msg, sizeof r->msg, "out of memory");
        oyl_arena_free(a);
        return;
    }
    oyl_parser_set_max_events(p, 0);
    oyl_parser_set_max_depth(p, max_depth);
    oyl_parser_set_dialect(p, oyl_dialect_goyaml2());
    oyl_str json;
    r->status = oyl_parse_json(p, NULL, strict ? OYL_JSON_STRICT : 0, &json);
    if (r->status == OYL_OK) {
        if (!json.data) json = (oyl_str){ "null", 4 };
        r->json = malloc(json.len);
        if (r->json) {
            memcpy(r->json, json.data, json.len);
            r->len = json.len;
        } else {
            r->status = OYL_ERR_MEMORY;
            snprintf(r->msg, sizeof r->msg, "out of memory");
        }
    } else {
        const char *m = oyl_parser_error(p);
        snprintf(r->msg, sizeof r->msg, "%s", m ? m : "invalid YAML");
        oyl_mark at = oyl_parser_error_mark(p);
        r->line = at.line;
        r->col = at.col;
    }
    oyl_parser_free(p);
    oyl_arena_free(a);
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// MaxDepth is the deepest nesting YAMLToJSON accepts, aliases expanded.
// go-yaml v2 allows 10,000; Oyl's parser recurses on some input, and cgo
// runs it on threads whose stacks may be small, so the default is Oyl's.
const MaxDepth = 256

// Error is YAML that couldn't be converted, with where (1-based).
type Error struct {
	Line, Column int
	Msg          string
}

func (e *Error) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("yaml: line %d: %s", e.Line, e.Msg)
	}
	return "yaml: " + e.Msg
}

// YAMLToJSON converts the first YAML document in y to JSON, byte for byte as
// sigs.k8s.io/yaml's YAMLToJSON does. A repeated key keeps its last value.
func YAMLToJSON(y []byte) ([]byte, error) {
	return convert(y, false)
}

// YAMLToJSONStrict is YAMLToJSON, but a repeated key is an error, as in
// sigs.k8s.io/yaml's YAMLToJSONStrict.
func YAMLToJSONStrict(y []byte) ([]byte, error) {
	return convert(y, true)
}

func convert(y []byte, strict bool) ([]byte, error) {
	var r C.oyl_go_result
	var in *C.char
	if len(y) > 0 {
		in = (*C.char)(unsafe.Pointer(&y[0]))
	}
	s := C.int(0)
	if strict {
		s = 1
	}
	C.oyl_go_yaml_to_json(in, C.size_t(len(y)), s, C.int(MaxDepth), &r)
	runtime.KeepAlive(y)
	if r.status != C.OYL_OK {
		return nil, &Error{Line: int(r.line), Column: int(r.col), Msg: C.GoString(&r.msg[0])}
	}
	out := C.GoBytes(unsafe.Pointer(r.json), C.int(r.len))
	C.free(unsafe.Pointer(r.json))
	return out, nil
}

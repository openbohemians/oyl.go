package oyl

/*
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "oyl/oyl.h"

// The events Go sends, one byte each, then their arguments: a string is
// a length (uint32, native order) and its bytes.
enum {
    OYL_GO_DOC = 1,     // a document
    OYL_GO_DOC_END,
    OYL_GO_PLAIN,       // text written as given: a number, a bool, null
    OYL_GO_STRING,      // a string, quoted where a reader would type it
    OYL_GO_TAGGED,      // a tag, then text
    OYL_GO_SEQ,         // then 1 for flow style, else 0
    OYL_GO_SEQ_END,
    OYL_GO_MAP,         // then 1 for flow style, else 0
    OYL_GO_MAP_END,
};

typedef struct {
    char  *out;     // malloc'd, for Go to copy and free
    size_t len;
    int    status;
    char   msg[200];
} oyl_go_emitted;

static int oyl_go_same(oyl_str a, oyl_str b) {
    return a.len == b.len && (a.len == 0 || !memcmp(a.data, b.data, a.len));
}

// Whether YAML 1.2, YAML 1.1 or go-yaml v2 reads text, written plain, as
// anything but a string.
static int oyl_go_typed(oyl_str text) {
    const oyl_schema *schemas[] = { oyl_schema_core(), oyl_schema_yaml11(), oyl_schema_goyaml2() };
    for (int k = 0; k < 3; k++)
        if (!oyl_go_same(oyl_schema_resolve(schemas[k], text, OYL_SCALAR_PLAIN), OYL_TAG_STR)) return 1;
    return 0;
}

static oyl_str oyl_go_read_str(const unsigned char **at) {
    uint32_t n;
    memcpy(&n, *at, 4);
    oyl_str s = { (const char *)*at + 4, n };
    *at += 4 + n;
    return s;
}

static void oyl_go_emit(const unsigned char *ev, size_t len, int indent, oyl_go_emitted *r) {
    memset(r, 0, sizeof *r);
    oyl_arena *a = oyl_arena_new(len + 4096);
    oyl_emitter *e = a ? oyl_emitter_new(a) : NULL;
    if (!e) {
        r->status = OYL_ERR_MEMORY;
        snprintf(r->msg, sizeof r->msg, "out of memory");
        oyl_arena_free(a);
        return;
    }
    oyl_emitter_set_indent(e, indent);
    oyl_status st = oyl_emit_stream_start(e);
    const unsigned char *at = ev, *end = ev + len;
    while (st == OYL_OK && at < end) {
        switch (*at++) {
        case OYL_GO_DOC:     st = oyl_emit_document_start(e, true); break;
        case OYL_GO_DOC_END: st = oyl_emit_document_end(e, true); break;
        case OYL_GO_PLAIN:
            st = oyl_emit_scalar(e, oyl_go_read_str(&at), OYL_SCALAR_PLAIN, OYL_STR_NULL, OYL_STR_NULL);
            break;
        case OYL_GO_STRING: {
            oyl_str s = oyl_go_read_str(&at);
            oyl_scalar_style style = s.len && memchr(s.data, '\n', s.len) ? OYL_SCALAR_LITERAL
                                   : oyl_go_typed(s) ? OYL_SCALAR_DOUBLE_QUOTED : OYL_SCALAR_PLAIN;
            st = oyl_emit_scalar(e, s, style, OYL_STR_NULL, OYL_STR_NULL);
            break;
        }
        case OYL_GO_TAGGED: {
            oyl_str tag = oyl_go_read_str(&at);
            st = oyl_emit_scalar(e, oyl_go_read_str(&at), OYL_SCALAR_PLAIN, OYL_STR_NULL, tag);
            break;
        }
        case OYL_GO_SEQ:     st = oyl_emit_sequence_start(e, OYL_STR_NULL, OYL_STR_NULL, *at++ != 0); break;
        case OYL_GO_SEQ_END: st = oyl_emit_sequence_end(e); break;
        case OYL_GO_MAP:     st = oyl_emit_mapping_start(e, OYL_STR_NULL, OYL_STR_NULL, *at++ != 0); break;
        case OYL_GO_MAP_END: st = oyl_emit_mapping_end(e); break;
        default:             st = OYL_ERR_INPUT; break;
        }
    }
    if (st == OYL_OK) st = oyl_emit_stream_end(e);
    if (st == OYL_OK) {
        oyl_str out = oyl_emitter_output(e);
        r->out = malloc(out.len ? out.len : 1);
        if (out.data && r->out) {
            memcpy(r->out, out.data, out.len);
            r->len = out.len;
        } else {
            st = OYL_ERR_MEMORY;
        }
    }
    if (st != OYL_OK) {
        free(r->out);
        r->out = NULL;
        snprintf(r->msg, sizeof r->msg, "%s", st == OYL_ERR_MEMORY ? "out of memory" : oyl_status_str(st));
    }
    r->status = st;
    oyl_emitter_free(e);
    oyl_arena_free(a);
}
*/
import "C"

import (
	"bytes"
	"encoding"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"
)

// Marshal writes in as a YAML 1.2 document, in block style with a 2-space
// indent. What it writes reads back as the same data in YAML 1.2, and in
// YAML 1.1 and go-yaml v2 readers alike: a string any of them would read
// as another type is quoted ("yes", "on", "y", "017", "1:30", ""), text
// with line breaks is a literal block (|), and a float always has a dot
// (1.0, 1.0e+20) or is .inf, -.inf or .nan.
//
// Encoding follows go-yaml: struct fields by their yaml tags (else json
// tags, else names lowercased), with omitempty, flow and inline; map keys
// sorted; nil slices and maps as [] and {}. []byte is written as
// !!binary, time.Time as !!timestamp, time.Duration as its string. Types
// implementing Marshaler or encoding.TextMarshaler write themselves.
// Values nested deeper than MaxDepth, which Unmarshal wouldn't read back,
// are an error, and so catch cycles.
func Marshal(in any) ([]byte, error) {
	return marshal(in, 2)
}

// An Encoder writes values as the documents of a YAML stream.
type Encoder struct {
	w      io.Writer
	indent int
	docs   int
}

// NewEncoder returns an Encoder writing to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w, indent: 2}
}

// SetIndent sets the spaces per indentation level, 1 to 10 (default 2).
func (enc *Encoder) SetIndent(spaces int) {
	enc.indent = spaces
}

// Encode writes v as the stream's next document (see Marshal).
func (enc *Encoder) Encode(v any) error {
	yaml, err := marshal(v, enc.indent)
	if err != nil {
		return err
	}
	if enc.docs > 0 && !bytes.HasPrefix(yaml, []byte("---")) {
		yaml = append([]byte("---\n"), yaml...)
	}
	enc.docs++
	_, err = enc.w.Write(yaml)
	return err
}

// Close is there for go-yaml's Encoder: every document is written when
// it is encoded.
func (enc *Encoder) Close() error {
	return nil
}

func marshal(in any, indent int) ([]byte, error) {
	e := encoder{}
	e.buf = append(e.buf, C.OYL_GO_DOC)
	if err := e.value(reflect.ValueOf(in), false); err != nil {
		return nil, err
	}
	e.buf = append(e.buf, C.OYL_GO_DOC_END)
	var r C.oyl_go_emitted
	C.oyl_go_emit((*C.uchar)(unsafe.Pointer(&e.buf[0])), C.size_t(len(e.buf)), C.int(indent), &r)
	if r.status != C.OYL_OK {
		return nil, &Error{Msg: C.GoString(&r.msg[0])}
	}
	out := C.GoBytes(unsafe.Pointer(r.out), C.int(r.len))
	C.free(unsafe.Pointer(r.out))
	return out, nil
}

// encoder turns Go values into events for C.
type encoder struct {
	buf   []byte
	depth int
}

func (e *encoder) str(s string) {
	e.buf = binary.NativeEndian.AppendUint32(e.buf, uint32(len(s)))
	e.buf = append(e.buf, s...)
}

func (e *encoder) plain(s string) {
	e.buf = append(e.buf, C.OYL_GO_PLAIN)
	e.str(s)
}

func (e *encoder) string(s string) {
	e.buf = append(e.buf, C.OYL_GO_STRING)
	e.str(s)
}

func (e *encoder) tagged(tag, s string) {
	e.buf = append(e.buf, C.OYL_GO_TAGGED)
	e.str(tag)
	e.str(s)
}

func (e *encoder) start(op byte, flow bool) error {
	if e.depth++; e.depth > MaxDepth {
		return &Error{Msg: fmt.Sprintf("nesting deeper than %d (a cycle?)", MaxDepth)}
	}
	f := byte(0)
	if flow {
		f = 1
	}
	e.buf = append(e.buf, op, f)
	return nil
}

func (e *encoder) end(op byte) {
	e.depth--
	e.buf = append(e.buf, op)
}

var (
	marshalerType     = reflect.TypeFor[Marshaler]()
	textMarshalerType = reflect.TypeFor[encoding.TextMarshaler]()
)

func (e *encoder) value(v reflect.Value, flow bool) error {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil() {
		v = v.Elem() // a pointer's methods are found again below: its target is addressable
	}
	if !v.IsValid() || ((v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && v.IsNil()) {
		e.plain("null")
		return nil
	}
	t := v.Type()
	if !t.Implements(marshalerType) && !t.Implements(textMarshalerType) && v.CanAddr() {
		if pt := reflect.PointerTo(t); pt.Implements(marshalerType) || pt.Implements(textMarshalerType) {
			v, t = v.Addr(), pt
		}
	}
	switch {
	case t.Implements(marshalerType):
		out, err := v.Interface().(Marshaler).MarshalYAML()
		if err != nil {
			return err
		}
		return e.value(reflect.ValueOf(out), flow)
	case t == timeType:
		e.tagged("tag:yaml.org,2002:timestamp", v.Interface().(time.Time).Format(time.RFC3339Nano))
		return nil
	case t == durationType:
		e.string(time.Duration(v.Int()).String())
		return nil
	case t.Implements(textMarshalerType):
		text, err := v.Interface().(encoding.TextMarshaler).MarshalText()
		if err != nil {
			return err
		}
		e.string(string(text))
		return nil
	}
	switch v.Kind() {
	case reflect.Bool:
		e.plain(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.plain(strconv.FormatInt(v.Int(), 10))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		e.plain(strconv.FormatUint(v.Uint(), 10))
	case reflect.Float32:
		e.plain(formatFloat(v.Float(), 32))
	case reflect.Float64:
		e.plain(formatFloat(v.Float(), 64))
	case reflect.String:
		e.string(v.String())
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 {
			e.tagged("tag:yaml.org,2002:binary", base64.StdEncoding.EncodeToString(v.Bytes()))
			return nil
		}
		return e.sequence(v, flow)
	case reflect.Array:
		return e.sequence(v, flow)
	case reflect.Map:
		return e.mapping(v, flow)
	case reflect.Struct:
		return e.structure(v, flow)
	default:
		return &Error{Msg: fmt.Sprintf("cannot marshal type %v", t)}
	}
	return nil
}

// formatFloat is Go's shortest float as every reader takes a float: with
// a dot, which YAML 1.1 wants before the exponent too.
func formatFloat(f float64, bits int) string {
	switch {
	case math.IsNaN(f):
		return ".nan"
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	}
	s := strconv.FormatFloat(f, 'g', -1, bits)
	mantissa, exponent, hasExponent := strings.Cut(s, "e")
	if strings.Contains(mantissa, ".") {
		return s
	}
	if hasExponent {
		return mantissa + ".0e" + exponent // Go signs the exponent
	}
	return s + ".0"
}

func (e *encoder) sequence(v reflect.Value, flow bool) error {
	if err := e.start(C.OYL_GO_SEQ, flow); err != nil {
		return err
	}
	for i := 0; i < v.Len(); i++ {
		if err := e.value(v.Index(i), flow); err != nil {
			return err
		}
	}
	e.end(C.OYL_GO_SEQ_END)
	return nil
}

func (e *encoder) mapping(v reflect.Value, flow bool) error {
	if err := e.start(C.OYL_GO_MAP, flow); err != nil {
		return err
	}
	keys := v.MapKeys()
	sort.Slice(keys, func(a, b int) bool { return keyLess(keys[a], keys[b]) })
	for _, k := range keys {
		if err := e.value(k, flow); err != nil {
			return err
		}
		if err := e.value(v.MapIndex(k), flow); err != nil {
			return err
		}
	}
	e.end(C.OYL_GO_MAP_END)
	return nil
}

func (e *encoder) structure(v reflect.Value, flow bool) error {
	info, err := getStructInfo(v.Type())
	if err != nil {
		return err
	}
	if err := e.start(C.OYL_GO_MAP, flow); err != nil {
		return err
	}
	for _, f := range info.fields {
		fv, ok := fieldGet(v, f.index)
		if !ok || (f.omitEmpty && isZero(fv)) {
			continue
		}
		e.string(f.name)
		if err := e.value(fv, flow || f.flow); err != nil {
			return err
		}
	}
	if m, ok := fieldGet(v, info.inlineMap); ok && info.inlineMap != nil {
		keys := m.MapKeys()
		sort.Slice(keys, func(a, b int) bool { return keys[a].String() < keys[b].String() })
		for _, k := range keys {
			if _, dup := info.byName[k.String()]; dup {
				return &Error{Msg: fmt.Sprintf("key %q in the inlined map of %v is also a field", k.String(), v.Type())}
			}
			e.string(k.String())
			if err := e.value(m.MapIndex(k), flow); err != nil {
				return err
			}
		}
	}
	e.end(C.OYL_GO_MAP_END)
	return nil
}

// fieldGet is v's field at index; false if an embedded pointer on the way
// is nil.
func fieldGet(v reflect.Value, index []int) (reflect.Value, bool) {
	for k, i := range index {
		if k > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return reflect.Value{}, false
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v, true
}

type isZeroer interface {
	IsZero() bool
}

// isZero is omitempty's test, as go-yaml has it.
func isZero(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return true
		}
	}
	if v.CanInterface() {
		if z, ok := v.Interface().(isZeroer); ok {
			return z.IsZero()
		}
	}
	switch v.Kind() {
	case reflect.String, reflect.Slice, reflect.Map, reflect.Array:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).PkgPath == "" && !isZero(v.Field(i)) {
				return false
			}
		}
		return true
	}
	return false
}

// keyLess orders map keys: null, then booleans, numbers by value,
// strings, and anything else by its printed form.
func keyLess(a, b reflect.Value) bool {
	for a.Kind() == reflect.Interface || a.Kind() == reflect.Pointer {
		if a.IsNil() {
			break
		}
		a = a.Elem()
	}
	for b.Kind() == reflect.Interface || b.Kind() == reflect.Pointer {
		if b.IsNil() {
			break
		}
		b = b.Elem()
	}
	ra, rb := keyRank(a), keyRank(b)
	if ra != rb {
		return ra < rb
	}
	switch ra {
	case 1:
		return !a.Bool() && b.Bool()
	case 2:
		return numLess(a, b)
	case 3:
		return a.String() < b.String()
	case 4:
		return fmt.Sprint(a.Interface()) < fmt.Sprint(b.Interface())
	}
	return false
}

func keyRank(v reflect.Value) int {
	switch v.Kind() {
	case reflect.Invalid, reflect.Interface, reflect.Pointer:
		return 0
	case reflect.Bool:
		return 1
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return 2
	case reflect.String:
		return 3
	}
	return 4
}

func numLess(a, b reflect.Value) bool {
	isInt := func(v reflect.Value) bool { return v.CanInt() }
	switch {
	case isInt(a) && isInt(b):
		return a.Int() < b.Int()
	case a.CanUint() && b.CanUint():
		return a.Uint() < b.Uint()
	case isInt(a) && b.CanUint():
		return a.Int() < 0 || uint64(a.Int()) < b.Uint()
	case a.CanUint() && isInt(b):
		return b.Int() >= 0 && a.Uint() < uint64(b.Int())
	}
	return toFloat(a) < toFloat(b)
}

func toFloat(v reflect.Value) float64 {
	switch {
	case v.CanInt():
		return float64(v.Int())
	case v.CanUint():
		return float64(v.Uint())
	}
	return v.Float()
}

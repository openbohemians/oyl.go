package oyl

/*
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "oyl/oyl.h"

// What Go needs of a node besides the node itself, worked out in C so
// that a document costs one cgo call.
typedef struct {
    uint64_t bits;     // a scalar's value: bool, int64, uint64 or the float's bits
    uint32_t eoff;     // a mapping: where its pairs start in pairs; on a repeated key, that key
    uint32_t elen;     // a mapping: its pairs
    uint8_t  vkind;    // a scalar's oyl_value_kind
    uint8_t  status;   // a scalar's value or a mapping's pairs: oyl_status
    uint8_t  tagk;     // a scalar of text: 1 a timestamp, 2 binary
} oyl_go_aux;

typedef struct {
    oyl_arena  *arena, *scratch;
    oyl_parser *parser;
    char       *input;           // a copy: the parser reads it across calls
    size_t      len;
    oyl_tree   *tree;            // the current document
    const oyl_node *nodes;
    uint32_t    count;
    const oyl_node_props *props;
    uint32_t    nprops;
    oyl_str     pool;
    size_t      base;
    int         dialect;         // which typed the document: 0 YAML 1.2, 1 YAML 1.1, 2 go-yaml v2
    oyl_go_aux *aux;
    uint32_t    aux_cap;
    uint32_t   *pairs;           // every mapping's key and value nodes
    size_t      npairs, pairs_cap;
    size_t      line, col;
    char        msg[320];
} oyl_go_doc;

static const oyl_dialect *oyl_go_dialect(int d) {
    return d == 1 ? oyl_dialect_yaml11() : d == 2 ? oyl_dialect_goyaml2() : oyl_dialect_yaml12();
}

static void oyl_go_close(oyl_go_doc *d) {
    if (!d) return;
    oyl_tree_free(d->tree);
    oyl_parser_free(d->parser);
    oyl_arena_free(d->arena);
    oyl_arena_free(d->scratch);
    free(d->input);
    free(d->aux);
    free(d->pairs);
    free(d);
}

static oyl_go_doc *oyl_go_open(const char *in, size_t len, int dialect, int max_depth) {
    oyl_go_doc *d = calloc(1, sizeof *d);
    if (!d) return NULL;
    d->input = malloc(len ? len : 1);
    d->arena = oyl_arena_new(len / 2 + 4096);
    d->scratch = oyl_arena_new(4096);
    if (!d->input || !d->arena || !d->scratch) {
        oyl_go_close(d);
        return NULL;
    }
    if (len) memcpy(d->input, in, len);
    d->len = len;
    d->parser = oyl_parser_new(d->input, len, d->arena);
    if (!d->parser) {
        oyl_go_close(d);
        return NULL;
    }
    oyl_parser_set_max_events(d->parser, 0);   // a tree keeps aliases: its events don't grow
    oyl_parser_set_max_depth(d->parser, max_depth);
    oyl_parser_set_dialect(d->parser, oyl_go_dialect(dialect));
    return d;
}

static int oyl_go_fail(oyl_go_doc *d, oyl_status st, const char *msg) {
    snprintf(d->msg, sizeof d->msg, "%s", msg);
    d->line = d->col = 0;
    return st;
}

// Reads the next document into d's tree and works out its values and
// pairs. At the end of the stream: OYL_OK with no tree.
static int oyl_go_next(oyl_go_doc *d) {
    oyl_tree_free(d->tree);
    d->tree = NULL;
    d->count = 0;
    oyl_status st = oyl_parse_tree(d->parser, &d->tree);
    if (st != OYL_OK) {
        const char *m = oyl_parser_error(d->parser);
        snprintf(d->msg, sizeof d->msg, "%s", m ? m : "invalid YAML");
        oyl_mark at = oyl_parser_error_mark(d->parser);
        d->line = at.line;
        d->col = at.col;
        return st;
    }
    if (!d->tree) return OYL_OK;
    d->nodes = oyl_tree_nodes(d->tree, &d->count);
    d->props = oyl_tree_props(d->tree, &d->nprops);
    d->pool = oyl_tree_pool(d->tree);
    d->base = oyl_tree_base(d->tree);
    const oyl_dialect *dialect = oyl_tree_dialect(d->tree);
    d->dialect = dialect == oyl_dialect_yaml11() ? 1 : dialect == oyl_dialect_goyaml2() ? 2 : 0;
    if (d->count > d->aux_cap) {
        oyl_go_aux *aux = realloc(d->aux, (size_t)d->count * sizeof *aux);
        if (!aux) return oyl_go_fail(d, OYL_ERR_MEMORY, "out of memory");
        d->aux = aux;
        d->aux_cap = d->count;
    }
    d->npairs = 0;
    for (uint32_t i = 0; i < d->count; i++) {
        oyl_go_aux *a = &d->aux[i];
        memset(a, 0, sizeof *a);
        if (d->nodes[i].kind == OYL_NODE_SCALAR) {
            oyl_value v;
            a->status = (uint8_t)oyl_node_value(d->tree, i, &v);
            if (a->status != OYL_OK) continue;
            a->vkind = (uint8_t)v.kind;
            switch (v.kind) {
            case OYL_VALUE_BOOL:  a->bits = v.as.b; break;
            case OYL_VALUE_INT:   memcpy(&a->bits, &v.as.i, 8); break;
            case OYL_VALUE_UINT:  a->bits = v.as.u; break;
            case OYL_VALUE_FLOAT: memcpy(&a->bits, &v.as.f, 8); break;
            case OYL_VALUE_STR: {
                oyl_str tag = oyl_node_tag(d->tree, i);
                if (tag.len == OYL_TAG_TIMESTAMP.len && !memcmp(tag.data, OYL_TAG_TIMESTAMP.data, tag.len)) a->tagk = 1;
                else if (tag.len == OYL_TAG_BINARY.len && !memcmp(tag.data, OYL_TAG_BINARY.data, tag.len)) a->tagk = 2;
                break;
            }
            default: break;
            }
        } else if (d->nodes[i].kind == OYL_NODE_MAPPING) {
            const oyl_pair *p;
            size_t n;
            oyl_status es = oyl_node_entries(d->tree, i, OYL_ENTRIES_STRICT, d->scratch, &p, &n);
            if (es != OYL_OK) {
                a->status = (uint8_t)es;
                a->eoff = es == OYL_ERR_PARSE ? (uint32_t)n : 0;
                oyl_arena_reset(d->scratch);
                continue;
            }
            if (d->npairs + 2 * n > d->pairs_cap) {
                size_t cap = d->pairs_cap ? d->pairs_cap : 256;
                while (cap < d->npairs + 2 * n) cap *= 2;
                uint32_t *pairs = realloc(d->pairs, cap * sizeof *pairs);
                if (!pairs) return oyl_go_fail(d, OYL_ERR_MEMORY, "out of memory");
                d->pairs = pairs;
                d->pairs_cap = cap;
            }
            a->eoff = (uint32_t)(d->npairs / 2);
            a->elen = (uint32_t)n;
            for (size_t k = 0; k < n; k++) {
                d->pairs[d->npairs++] = p[k].key;
                d->pairs[d->npairs++] = p[k].value;
            }
            oyl_arena_reset(d->scratch);
        }
    }
    return OYL_OK;
}

static void oyl_go_mark(oyl_go_doc *d, uint32_t i, size_t *line, size_t *col) {
    oyl_mark m = oyl_node_mark(d->tree, i);
    *line = m.line;
    *col = m.col;
}
*/
import "C"

import (
	"encoding"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"reflect"
	"runtime"
	"strings"
	"time"
	"unsafe"
)

// Unmarshal decodes the first YAML document in in into out, which must be
// a non-nil pointer. Scalars are typed by YAML 1.2 unless an option says
// otherwise; aliases are followed and << merge keys applied; a key
// written twice in a mapping is an error. An empty input leaves out as it
// is.
//
// Decoding follows go-yaml: a struct's keys are its fields' yaml tags
// (else json tags, else the names lowercased), a mapping read into an
// interface is a map[string]any when its keys are strings (else
// map[any]any), an integer an int (else int64 or uint64), and null sets
// the zero value. A string takes any scalar's text. Types implementing
// Unmarshaler or encoding.TextUnmarshaler decode themselves; time.Time
// reads timestamps and time.Duration Go durations ("1m30s").
func Unmarshal(in []byte, out any, opts ...Option) error {
	o := options{}
	for _, opt := range opts {
		opt(&o)
	}
	d, err := openDoc(in, o)
	if err != nil {
		return err
	}
	defer d.close()
	ok, err := d.next()
	if err != nil || !ok {
		return err
	}
	return d.decodeRoot(out)
}

// A Decoder reads the documents of a YAML stream one by one.
type Decoder struct {
	r    io.Reader
	opts options
	doc  *doc
	err  error
}

// NewDecoder returns a Decoder reading from r, which it reads to the end
// at the first Decode.
func NewDecoder(r io.Reader, opts ...Option) *Decoder {
	dec := &Decoder{r: r}
	for _, opt := range opts {
		opt(&dec.opts)
	}
	return dec
}

// KnownFields makes a key that matches no field of the struct it is
// decoded into an error, as WithKnownFields does.
func (dec *Decoder) KnownFields(enable bool) {
	dec.opts.knownFields = enable
	if dec.doc != nil {
		dec.doc.knownFields = enable
	}
}

// Decode decodes the next document into v (see Unmarshal). After the last
// it returns io.EOF.
func (dec *Decoder) Decode(v any) error {
	if dec.err != nil {
		return dec.err
	}
	if dec.doc == nil {
		in, err := io.ReadAll(dec.r)
		if err != nil {
			dec.err = err
			return err
		}
		if dec.doc, err = openDoc(in, dec.opts); err != nil {
			dec.err = err
			return err
		}
	}
	ok, err := dec.doc.next()
	if err == nil && !ok {
		err = io.EOF
	}
	if err != nil {
		dec.err = err
		dec.doc.close()
		return err
	}
	return dec.doc.decodeRoot(v)
}

// A document read into C: its nodes and what was worked out for them,
// read from Go in place.
type doc struct {
	c           *C.oyl_go_doc
	input       []byte // the C copy
	nodes       []C.oyl_node
	aux         []C.oyl_go_aux
	props       []C.oyl_node_props
	pairs       []uint32
	pool        []byte
	base        int
	dialect     Dialect
	knownFields bool
	budget      int // nodes left to visit, aliases followed
	depth       int
}

func openDoc(in []byte, o options) (*doc, error) {
	var p *C.char
	if len(in) > 0 {
		p = (*C.char)(unsafe.Pointer(&in[0]))
	}
	c := C.oyl_go_open(p, C.size_t(len(in)), C.int(o.dialect), C.int(MaxDepth))
	if c == nil {
		return nil, &Error{Msg: "out of memory"}
	}
	d := &doc{c: c, knownFields: o.knownFields}
	if len(in) > 0 {
		d.input = unsafe.Slice((*byte)(unsafe.Pointer(c.input)), len(in))
	}
	runtime.SetFinalizer(d, (*doc).close) // a Decoder left before the end
	return d, nil
}

func (d *doc) close() {
	if d.c != nil {
		C.oyl_go_close(d.c)
		d.c = nil
		d.input, d.nodes, d.aux, d.props, d.pairs, d.pool = nil, nil, nil, nil, nil, nil
	}
}

// next reads the next document; false at the end of the stream.
func (d *doc) next() (bool, error) {
	if d.c == nil {
		return false, nil
	}
	if st := C.oyl_go_next(d.c); st != C.OYL_OK {
		return false, &Error{Line: int(d.c.line), Column: int(d.c.col), Msg: C.GoString(&d.c.msg[0])}
	}
	if d.c.tree == nil {
		return false, nil
	}
	c := d.c
	d.nodes = unsafe.Slice(c.nodes, c.count)
	d.aux = unsafe.Slice(c.aux, c.count)
	d.props = unsafe.Slice(c.props, c.nprops)
	d.pairs = unsafe.Slice((*uint32)(unsafe.Pointer(c.pairs)), c.npairs)
	d.pool = unsafe.Slice((*byte)(unsafe.Pointer(c.pool.data)), c.pool.len)
	d.base = int(c.base)
	d.dialect = Dialect(c.dialect)
	d.budget = 100*int(c.count) + 1_000_000
	d.depth = 0
	return true, nil
}

func (d *doc) decodeRoot(out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return &Error{Msg: fmt.Sprintf("cannot decode into %T: want a non-nil pointer", out)}
	}
	return d.decode(0, v.Elem())
}

// ── Reading nodes ───────────────────────────────────────────

func (d *doc) kind(i uint32) C.uint8_t { return d.nodes[i].kind }

func (d *doc) text(i uint32) string {
	n := &d.nodes[i]
	if n.flags&C.OYL_NODE_POOLED != 0 {
		return string(d.pool[n.text : n.text+n.len])
	}
	at := d.base + int(n.text)
	return string(d.input[at : at+int(n.len)])
}

// tag is the tag written on a node, or "".
func (d *doc) tag(i uint32) string {
	n := &d.nodes[i]
	if n.kind == C.OYL_NODE_ALIAS || n.props == 0 {
		return ""
	}
	p := &d.props[n.props]
	return string(d.pool[p.tag : p.tag+p.tag_len])
}

func (d *doc) isNull(i uint32) bool {
	return d.kind(i) == C.OYL_NODE_SCALAR && d.aux[i].status == C.OYL_OK && d.aux[i].vkind == C.OYL_VALUE_NULL
}

// shortTag names a node's type as errors give it: !!str, !!int, !!seq.
func (d *doc) shortTag(i uint32) string {
	if tag := d.tag(i); tag != "" {
		if s, ok := strings.CutPrefix(tag, "tag:yaml.org,2002:"); ok {
			return "!!" + s
		}
		return tag
	}
	switch d.kind(i) {
	case C.OYL_NODE_SEQUENCE:
		return "!!seq"
	case C.OYL_NODE_MAPPING:
		return "!!map"
	}
	a := &d.aux[i]
	switch {
	case a.tagk == 1:
		return "!!timestamp"
	case a.tagk == 2:
		return "!!binary"
	}
	switch a.vkind {
	case C.OYL_VALUE_NULL:
		return "!!null"
	case C.OYL_VALUE_BOOL:
		return "!!bool"
	case C.OYL_VALUE_INT, C.OYL_VALUE_UINT:
		return "!!int"
	case C.OYL_VALUE_FLOAT:
		return "!!float"
	}
	return "!!str"
}

func (d *doc) errorf(i uint32, format string, args ...any) error {
	var line, col C.size_t
	C.oyl_go_mark(d.c, C.uint32_t(i), &line, &col)
	return &Error{Line: int(line), Column: int(col), Msg: fmt.Sprintf(format, args...)}
}

func (d *doc) typeError(i uint32, t reflect.Type) error {
	if d.kind(i) == C.OYL_NODE_SCALAR {
		text := d.text(i)
		if len(text) > 40 {
			text = text[:37] + "..."
		}
		return d.errorf(i, "cannot unmarshal %s `%s` into %v", d.shortTag(i), text, t)
	}
	return d.errorf(i, "cannot unmarshal %s into %v", d.shortTag(i), t)
}

// visit counts a node against the budget, which aliases can't outrun.
func (d *doc) visit(i uint32) error {
	if d.budget--; d.budget < 0 {
		return d.errorf(i, "document is too large after expanding aliases")
	}
	return nil
}

func (d *doc) enter(i uint32) error {
	if d.depth++; d.depth > MaxDepth {
		return d.errorf(i, "nesting deeper than %d", MaxDepth)
	}
	return nil
}

// items are a sequence's item nodes.
func (d *doc) items(i uint32) []uint32 {
	n := int(d.nodes[i].len)
	items := make([]uint32, n)
	j := i + 1
	for k := 0; k < n; k++ {
		items[k] = j
		j += uint32(d.nodes[j].span)
	}
	return items
}

// entries are a mapping's key and value nodes, merges applied.
func (d *doc) entries(i uint32) ([]uint32, error) {
	a := &d.aux[i]
	switch a.status {
	case C.OYL_OK:
		return d.pairs[2*a.eoff : 2*(a.eoff+a.elen)], nil
	case C.OYL_ERR_PARSE:
		return nil, d.errorf(uint32(a.eoff), "mapping key %q already defined", d.text(uint32(a.eoff)))
	case C.OYL_ERR_LIMIT:
		return nil, d.errorf(i, "merges too costly to work out")
	}
	return nil, d.errorf(i, "out of memory")
}

// ── Decoding ────────────────────────────────────────────────

var (
	timeType            = reflect.TypeFor[time.Time]()
	durationType        = reflect.TypeFor[time.Duration]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
	unmarshalerType     = reflect.TypeFor[Unmarshaler]()
)

func (d *doc) decode(i uint32, out reflect.Value) error {
	if err := d.visit(i); err != nil {
		return err
	}
	if d.kind(i) == C.OYL_NODE_ALIAS {
		return d.decode(uint32(d.nodes[i].props), out)
	}
	if d.isNull(i) {
		out.SetZero()
		return nil
	}
	if out.Kind() == reflect.Pointer {
		if out.IsNil() {
			out.Set(reflect.New(out.Type().Elem()))
		}
		return d.decode(i, out.Elem())
	}
	if out.CanAddr() && reflect.PointerTo(out.Type()).Implements(unmarshalerType) {
		u := out.Addr().Interface().(Unmarshaler)
		return u.UnmarshalYAML(func(v any) error {
			rv := reflect.ValueOf(v)
			if rv.Kind() != reflect.Pointer || rv.IsNil() {
				return &Error{Msg: fmt.Sprintf("cannot decode into %T: want a non-nil pointer", v)}
			}
			return d.decode(i, rv.Elem())
		})
	}
	if out.Kind() == reflect.Interface {
		if out.NumMethod() > 0 {
			if !out.IsNil() && out.Elem().Kind() == reflect.Pointer {
				return d.decode(i, out.Elem())
			}
			return d.typeError(i, out.Type())
		}
		v, err := d.generic(i)
		if err != nil {
			return err
		}
		if v == nil {
			out.SetZero()
		} else {
			out.Set(reflect.ValueOf(v))
		}
		return nil
	}
	switch d.kind(i) {
	case C.OYL_NODE_SCALAR:
		return d.scalar(i, out)
	case C.OYL_NODE_SEQUENCE:
		return d.sequence(i, out)
	}
	return d.mapping(i, out)
}

func (d *doc) scalar(i uint32, out reflect.Value) error {
	a := &d.aux[i]
	if a.status != C.OYL_OK {
		return d.errorf(i, "invalid %s `%s`", d.shortTag(i), d.text(i))
	}
	text := d.text(i)
	switch out.Type() {
	case timeType:
		t, ok := parseTimestamp(text)
		if !ok || a.vkind != C.OYL_VALUE_STR {
			return d.typeError(i, out.Type())
		}
		out.Set(reflect.ValueOf(t))
		return nil
	case durationType:
		if a.vkind == C.OYL_VALUE_STR {
			dur, err := time.ParseDuration(text)
			if err != nil {
				return d.errorf(i, "cannot unmarshal %s `%s` into time.Duration: %v", d.shortTag(i), text, err)
			}
			out.SetInt(int64(dur))
			return nil
		}
		return d.typeError(i, out.Type())
	}
	if out.CanAddr() && reflect.PointerTo(out.Type()).Implements(textUnmarshalerType) {
		if a.tagk == 2 {
			b, err := d.binary(i)
			if err != nil {
				return err
			}
			text = string(b)
		}
		if err := out.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(text)); err != nil {
			return d.errorf(i, "%v", err)
		}
		return nil
	}
	switch out.Kind() {
	case reflect.String:
		if a.tagk == 2 {
			b, err := d.binary(i)
			if err != nil {
				return err
			}
			text = string(b)
		}
		out.SetString(text)
		return nil
	case reflect.Bool:
		if a.vkind == C.OYL_VALUE_BOOL {
			out.SetBool(a.bits != 0)
			return nil
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		switch a.vkind {
		case C.OYL_VALUE_INT:
			if n := int64(a.bits); !out.OverflowInt(n) {
				out.SetInt(n)
				return nil
			}
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch a.vkind {
		case C.OYL_VALUE_INT, C.OYL_VALUE_UINT:
			if a.vkind == C.OYL_VALUE_UINT || int64(a.bits) >= 0 {
				if n := uint64(a.bits); !out.OverflowUint(n) {
					out.SetUint(n)
					return nil
				}
			}
		}
	case reflect.Float32, reflect.Float64:
		switch a.vkind {
		case C.OYL_VALUE_FLOAT:
			out.SetFloat(math.Float64frombits(uint64(a.bits)))
			return nil
		case C.OYL_VALUE_INT:
			out.SetFloat(float64(int64(a.bits)))
			return nil
		case C.OYL_VALUE_UINT:
			out.SetFloat(float64(uint64(a.bits)))
			return nil
		}
	case reflect.Slice:
		if out.Type().Elem().Kind() == reflect.Uint8 && a.vkind == C.OYL_VALUE_STR {
			b := []byte(text)
			if a.tagk == 2 {
				var err error
				if b, err = d.binary(i); err != nil {
					return err
				}
			}
			out.SetBytes(b)
			return nil
		}
	}
	return d.typeError(i, out.Type())
}

func (d *doc) binary(i uint32) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(d.text(i)), ""))
	if err != nil {
		return nil, d.errorf(i, "invalid !!binary: %v", err)
	}
	return b, nil
}

func (d *doc) sequence(i uint32, out reflect.Value) error {
	if err := d.enter(i); err != nil {
		return err
	}
	defer func() { d.depth-- }()
	items := d.items(i)
	switch out.Kind() {
	case reflect.Slice:
		s := reflect.MakeSlice(out.Type(), len(items), len(items))
		for k, item := range items {
			if err := d.decode(item, s.Index(k)); err != nil {
				return err
			}
		}
		out.Set(s)
		return nil
	case reflect.Array:
		if len(items) != out.Len() {
			return d.errorf(i, "cannot unmarshal %d items into %v", len(items), out.Type())
		}
		for k, item := range items {
			if err := d.decode(item, out.Index(k)); err != nil {
				return err
			}
		}
		return nil
	}
	return d.typeError(i, out.Type())
}

func (d *doc) mapping(i uint32, out reflect.Value) error {
	if err := d.enter(i); err != nil {
		return err
	}
	defer func() { d.depth-- }()
	switch out.Kind() {
	case reflect.Map:
		pairs, err := d.entries(i)
		if err != nil {
			return err
		}
		if out.IsNil() {
			out.Set(reflect.MakeMapWithSize(out.Type(), len(pairs)/2))
		}
		return d.mapPairs(pairs, out)
	case reflect.Struct:
		pairs, err := d.entries(i)
		if err != nil {
			return err
		}
		return d.structPairs(pairs, out)
	}
	return d.typeError(i, out.Type())
}

func (d *doc) mapPairs(pairs []uint32, m reflect.Value) error {
	kt, vt := m.Type().Key(), m.Type().Elem()
	for k := 0; k < len(pairs); k += 2 {
		key := reflect.New(kt).Elem()
		if err := d.decode(pairs[k], key); err != nil {
			return err
		}
		if !key.Comparable() || (key.Kind() == reflect.Interface && !key.IsNil() && !key.Elem().Comparable()) {
			return d.errorf(pairs[k], "invalid map key: %s", d.shortTag(d.target(pairs[k])))
		}
		val := reflect.New(vt).Elem()
		if err := d.decode(pairs[k+1], val); err != nil {
			return err
		}
		m.SetMapIndex(key, val)
	}
	return nil
}

func (d *doc) structPairs(pairs []uint32, out reflect.Value) error {
	info, err := getStructInfo(out.Type())
	if err != nil {
		return err
	}
	var inline reflect.Value
	for k := 0; k < len(pairs); k += 2 {
		var name string
		if err := d.decode(pairs[k], reflect.ValueOf(&name).Elem()); err != nil {
			return err
		}
		if f, ok := info.byName[name]; ok {
			if err := d.decode(pairs[k+1], fieldAlloc(out, info.fields[f].index)); err != nil {
				return err
			}
			continue
		}
		if info.inlineMap != nil {
			if !inline.IsValid() {
				inline = fieldAlloc(out, info.inlineMap)
				if inline.IsNil() {
					inline.Set(reflect.MakeMap(inline.Type()))
				}
			}
			val := reflect.New(inline.Type().Elem()).Elem()
			if err := d.decode(pairs[k+1], val); err != nil {
				return err
			}
			inline.SetMapIndex(reflect.ValueOf(name).Convert(inline.Type().Key()), val)
			continue
		}
		if d.knownFields {
			return d.errorf(pairs[k], "field %s not found in type %v", name, out.Type())
		}
	}
	return nil
}

// fieldAlloc is v's field at index, allocating the embedded pointers on
// the way.
func fieldAlloc(v reflect.Value, index []int) reflect.Value {
	for k, i := range index {
		if k > 0 && v.Kind() == reflect.Pointer {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

func (d *doc) target(i uint32) uint32 {
	if d.kind(i) == C.OYL_NODE_ALIAS {
		return uint32(d.nodes[i].props)
	}
	return i
}

// generic is a node as an interface value.
func (d *doc) generic(i uint32) (any, error) {
	if err := d.visit(i); err != nil {
		return nil, err
	}
	switch d.kind(i) {
	case C.OYL_NODE_ALIAS:
		return d.generic(uint32(d.nodes[i].props))
	case C.OYL_NODE_SEQUENCE:
		if err := d.enter(i); err != nil {
			return nil, err
		}
		defer func() { d.depth-- }()
		items := d.items(i)
		s := make([]any, len(items))
		for k, item := range items {
			v, err := d.generic(item)
			if err != nil {
				return nil, err
			}
			s[k] = v
		}
		return s, nil
	case C.OYL_NODE_MAPPING:
		if err := d.enter(i); err != nil {
			return nil, err
		}
		defer func() { d.depth-- }()
		pairs, err := d.entries(i)
		if err != nil {
			return nil, err
		}
		keys := make([]any, len(pairs)/2)
		strs := true
		for k := range keys {
			if keys[k], err = d.generic(pairs[2*k]); err != nil {
				return nil, err
			}
			if _, ok := keys[k].(string); !ok {
				strs = false
			}
		}
		if strs {
			m := make(map[string]any, len(keys))
			for k, key := range keys {
				if m[key.(string)], err = d.generic(pairs[2*k+1]); err != nil {
					return nil, err
				}
			}
			return m, nil
		}
		m := make(map[any]any, len(keys))
		for k, key := range keys {
			if key != nil && !reflect.TypeOf(key).Comparable() {
				return nil, d.errorf(pairs[2*k], "invalid map key: %s", d.shortTag(d.target(pairs[2*k])))
			}
			if m[key], err = d.generic(pairs[2*k+1]); err != nil {
				return nil, err
			}
		}
		return m, nil
	}
	a := &d.aux[i]
	if a.status != C.OYL_OK {
		return nil, d.errorf(i, "invalid %s `%s`", d.shortTag(i), d.text(i))
	}
	switch a.vkind {
	case C.OYL_VALUE_NULL:
		return nil, nil
	case C.OYL_VALUE_BOOL:
		return a.bits != 0, nil
	case C.OYL_VALUE_INT:
		if n := int64(a.bits); n == int64(int(n)) {
			return int(n), nil
		}
		return int64(a.bits), nil
	case C.OYL_VALUE_UINT:
		return uint64(a.bits), nil
	case C.OYL_VALUE_FLOAT:
		return math.Float64frombits(uint64(a.bits)), nil
	}
	switch a.tagk {
	case 1: // a timestamp: a time.Time, but as go-yaml v2 reads it, unless tagged
		if d.dialect != GoYAML2 || d.tag(i) != "" {
			if t, ok := parseTimestamp(d.text(i)); ok {
				return t, nil
			}
		}
	case 2:
		b, err := d.binary(i)
		return string(b), err
	}
	return d.text(i), nil
}

// The timestamps go-yaml reads.
var timestampFormats = []string{
	"2006-1-2T15:4:5.999999999Z07:00",
	"2006-1-2t15:4:5.999999999Z07:00",
	"2006-1-2 15:4:5.999999999",
	"2006-1-2",
}

func parseTimestamp(s string) (time.Time, bool) {
	// a cheap check first: a timestamp starts with a four-digit year
	if len(s) < 8 || s[4] != '-' || strings.IndexFunc(s[:4], func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return time.Time{}, false
	}
	for _, format := range timestampFormats {
		if t, err := time.Parse(format, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

package oyl

/*
#cgo CFLAGS: -std=c11 -O2 -I${SRCDIR}/c/include
#include <stdio.h>
#include <string.h>
#include "oyl/oyl.h"

typedef struct {
    oyl_arena     *arena;
    oyl_parser    *parser;
    oyl_str        out;
    const oyl_fix *fixes;
    size_t         nfixes;
    int            status;
    size_t         line, col;
    char           msg[320];
} oyl_go_upgrade_result;

// Upgrade yaml, read by reader (0: unknown, 1: YAML 1.1, 2: go-yaml v2);
// the output and fixes stay in r's arena until oyl_go_upgrade_free.
static void oyl_go_upgrade(const char *yaml, size_t len, int reader, unsigned flags, int max_depth,
                           oyl_go_upgrade_result *r) {
    memset(r, 0, sizeof *r);
    r->arena = oyl_arena_new(len + 4096);
    r->parser = r->arena ? oyl_parser_new(yaml ? yaml : "", len, r->arena) : NULL;
    if (!r->parser) {
        r->status = OYL_ERR_MEMORY;
        snprintf(r->msg, sizeof r->msg, "out of memory");
        return;
    }
    oyl_parser_set_max_depth(r->parser, max_depth);
    if (reader == 1) oyl_parser_set_dialect(r->parser, oyl_dialect_yaml11());
    if (reader == 2) oyl_parser_set_dialect(r->parser, oyl_dialect_goyaml2());
    r->status = oyl_upgrade(r->parser, flags, &r->out, &r->fixes, &r->nfixes);
    if (r->status != OYL_OK) {
        const char *m = oyl_parser_error(r->parser);
        snprintf(r->msg, sizeof r->msg, "%s", m ? m : "invalid YAML");
        oyl_mark at = oyl_parser_error_mark(r->parser);
        r->line = at.line;
        r->col = at.col;
    }
}

static void oyl_go_upgrade_free(oyl_go_upgrade_result *r) {
    oyl_parser_free(r->parser);
    oyl_arena_free(r->arena);
}
*/
import "C"

import (
	"runtime"
	"unsafe"
)

// UpgradeKeysKeepValue makes Upgrade give keys the reader's value too
// (on: becomes true:); by default keys keep their text, as YAML 1.2 reads
// it. Unmarshal ignores it.
func UpgradeKeysKeepValue() Option {
	return func(o *options) { o.keysKeepValue = true }
}

// UpgradeValuesKeepText makes Upgrade give values YAML 1.2's reading too
// (yes becomes "yes"); by default values keep the reader's value.
// Unmarshal ignores it.
func UpgradeValuesKeepText() Option {
	return func(o *options) { o.valuesKeepText = true }
}

// FixKind is what a Fix is.
type FixKind int

const (
	// FixScalar is a scalar YAML 1.2 reads otherwise.
	FixScalar FixKind = iota + 1
	// FixDirective is %YAML 1.1, removed.
	FixDirective
	// FixEscape is \' in double quotes (go-yaml v2's), the backslash removed.
	FixEscape
	// FixChomp is a block scalar ending the input without a line break.
	FixChomp
	// FixMerge is a key go-yaml v2's merge order overrides, commented out.
	FixMerge
	// FixIndent is a quoted scalar's or flow collection's line not indented
	// past its block, indented.
	FixIndent
)

// A Fix is a change Upgrade made, or a difference it found and left.
type Fix struct {
	Kind FixKind
	// Where, in the input (1-based; the column counts bytes)
	Line, Column int
	// The input's text there, and what replaced it (empty if removed, or left)
	Before, After string
	// False when only reported, the input left as it was
	Fixed bool
	// What each reader reads, and what was done
	Message string
}

// Upgrade makes in, a YAML 1.1 file, read the same in YAML 1.2, rewriting
// only what a 1.2 reader would read otherwise and keeping every other byte
// (comments, layout, quoting, key order). It returns the new text and each
// change, with what couldn't be fixed (reported, left as it was).
//
// WithDialect names the file's reader: YAML11 for PyYAML (Ansible), GoYAML2
// for go-yaml v2 (Kubernetes); afterwards the file reads the same in it and
// in YAML 1.2. Without it (or with YAML12) the reader is unknown: what
// PyYAML and go-yaml v2 read alike is fixed, what they read otherwise
// reported. Values keep the reader's meaning and keys their text, unless
// UpgradeKeysKeepValue or UpgradeValuesKeepText say otherwise.
//
// The error is an *Error for input the reader can't read, or if the output
// would read otherwise than decided (a bug, never a file).
func Upgrade(in []byte, opts ...Option) ([]byte, []Fix, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	reader := C.int(0)
	switch o.dialect {
	case YAML11:
		reader = 1
	case GoYAML2:
		reader = 2
	}
	flags := C.uint(0)
	if o.keysKeepValue {
		flags |= C.OYL_UPGRADE_KEYS_KEEP_VALUE
	}
	if o.valuesKeepText {
		flags |= C.OYL_UPGRADE_VALUES_KEEP_TEXT
	}
	var p *C.char
	if len(in) > 0 {
		p = (*C.char)(unsafe.Pointer(&in[0]))
	}
	var r C.oyl_go_upgrade_result
	C.oyl_go_upgrade(p, C.size_t(len(in)), reader, flags, C.int(MaxDepth), &r)
	runtime.KeepAlive(in)
	defer C.oyl_go_upgrade_free(&r)
	if r.status != C.OYL_OK {
		return nil, nil, &Error{Line: int(r.line), Column: int(r.col), Msg: C.GoString(&r.msg[0])}
	}
	out := C.GoBytes(unsafe.Pointer(r.out.data), C.int(r.out.len))
	var fixes []Fix
	if r.nfixes > 0 {
		for _, f := range unsafe.Slice(r.fixes, int(r.nfixes)) {
			fixes = append(fixes, Fix{
				Kind:    FixKind(f.kind),
				Line:    int(f.mark.line),
				Column:  int(f.mark.col),
				Before:  C.GoStringN(f.before.data, C.int(f.before.len)),
				After:   C.GoStringN(f.after.data, C.int(f.after.len)),
				Fixed:   bool(f.fixed),
				Message: C.GoString(f.message),
			})
		}
	}
	return out, fixes, nil
}

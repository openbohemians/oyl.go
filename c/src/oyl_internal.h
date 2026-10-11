/*
 * oyl_internal.h — Declarations shared between oyl's source files.
 *
 * Not installed. Everything here is hidden from the shared library's
 * exported symbols (the library is built with -fvisibility=hidden, and
 * only functions marked OYL_API in oyl.h are exported), so it can change
 * freely without affecting the ABI.
 */

#ifndef OYL_INTERNAL_H
#define OYL_INTERNAL_H

#include "oyl/oyl.h"

/* ── Schema ──────────────────────────────────────────────── */

/* How a rule parses a scalar */
typedef enum {
    RULE_WORD,         /* the text is `pattern`; the value is `value` */
    RULE_WORD_ICASE,   /* the same, ignoring ASCII case */
    RULE_INT,          /* the built-in int parser, with OYL_INT_ `flags` */
    RULE_FLOAT,        /* the built-in float parser, with OYL_FLOAT_ `flags` */
    RULE_TIMESTAMP,    /* go-yaml v2's timestamps; the value is the text */
    RULE_CUSTOM,       /* `parse(text, out, user)` */
    RULE_NONE,         /* never accepts (an unknown builtin name) */
} oyl_rule_kind;

/* A single tag resolution rule: a plain scalar the rule's parser accepts
 * resolves to `tag`, and the parser gives its value. */
typedef struct {
    oyl_rule_kind     kind;
    const char       *pattern;   /* the word, for RULE_WORD and RULE_WORD_ICASE */
    size_t            plen;      /* its length */
    oyl_str           tag;
    unsigned          flags;
    oyl_value         value;     /* a word's value (STR means the text) */
    oyl_scalar_parser parse;
    void             *user;
} oyl_schema_rule;

struct oyl_schema {
    const oyl_schema_rule *rules;
    int                    rule_count;
    oyl_str                default_plain_tag;   /* unmatched plain scalars */
    oyl_str                default_quoted_tag;  /* all quoted scalars */
    oyl_str                default_seq_tag;     /* untagged sequences */
    oyl_str                default_map_tag;     /* untagged mappings */
    /* the first bytes any rule could accept: a plain scalar starting with
     * another byte is the default at once (NULL: no shortcut) */
    const bool            *first;
};

/* ── Dialects and warnings ───────────────────────────────── */

struct oyl_dialect {
    const oyl_schema *schema;
    unsigned          syntax;   /* OYL_SYNTAX_ */
    oyl_merge_order   merge;
};

/* Where warnings go. The parser re-reads input when it falls back to its
 * eager mode, so a warning is reported only past the last one of its code. */
typedef struct {
    oyl_warn_fn fn;
    void       *user;
    size_t      last[8];   /* per code: the offset of the last one reported */
    bool        seen[8];
} oyl_warn_state;

/* oyl_schema_value from a scalar's parts (for oyl_json.c) */
oyl_status oyl_schema_value_of(const oyl_schema *schema, oyl_str text, oyl_scalar_style style,
                               oyl_str tag, oyl_value *out);

/* Report a warning from parsing, once (see oyl_warn_state) */
void oyl_warn(oyl_warn_state *w, oyl_warning code, oyl_mark mark, const char *msg);

/* ── Scanner ─────────────────────────────────────────────── */

/* Scan the next token into caller-provided storage. oyl_scan_next() wraps
 * this for the public API; the parser calls it directly. */
oyl_status oyl_scan_token(oyl_scanner *s, oyl_token *tok);
/* The syntax leniencies to apply (OYL_SYNTAX_), and where to warn of their
 * use; the parser sets them per document. */
void oyl_scanner_set_syntax(oyl_scanner *s, unsigned syntax, oyl_warn_state *w);
/* Copy one scanner's state into another over the same input (the parser's
 * checkpoint at a document start). */
bool oyl_scanner_copy(oyl_scanner *dst, const oyl_scanner *src);

/* ── Parser, for oyl_json.c ──────────────────────────────── */

/* Report an error found after parsing (a converter's), as parse errors
 * are: oyl_parser_error() and oyl_parser_error_mark() return it. */
void oyl_parser_fail(oyl_parser *p, oyl_mark mark, const char *msg);
oyl_arena        *oyl_parser_arena(const oyl_parser *p);
int               oyl_parser_depth_limit(const oyl_parser *p);   /* 0: none */
const oyl_schema *oyl_parser_schema(const oyl_parser *p);       /* NULL: none */
/* Report a warning found after parsing (no re-reading to guard against) */
void              oyl_parser_warn(oyl_parser *p, oyl_warning code, oyl_mark mark,
                                  const char *msg);

/* For small helpers on the per-token path that GCC may otherwise decline
 * to inline once they grow a little. */
#if defined(__GNUC__) || defined(__clang__)
#  define ALWAYS_INLINE static inline __attribute__((always_inline))
#else
#  define ALWAYS_INLINE static inline
#endif

/* For cold paths that would otherwise be inlined into a hot caller. */
#if defined(__GNUC__) || defined(__clang__)
#  define NOINLINE static __attribute__((noinline))
#else
#  define NOINLINE static
#endif

#endif /* OYL_INTERNAL_H */

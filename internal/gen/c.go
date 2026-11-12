package gen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// cTypes maps a base type to the record its struct member is (kvp_arena.h),
// its KVP_* constant, the suffix of the kvp_put_/kvp_read_/kvp_list_add_
// functions of a scalar ("" for what travels as bytes), and the plain C
// type and item reader of a scalar list element.
var cTypes = map[string]struct{ st, kvp, fn, plain, item string }{
	schema.Int32:    {"kvp_int32_st", "KVP_INT32", "int32", "int32_t", "kvp_int32"},
	schema.Int64:    {"kvp_int64_st", "KVP_INT64", "int64", "int64_t", "kvp_int64"},
	schema.TS:       {"kvp_ts_st", "KVP_TS", "ts", "int64_t", "kvp_ts"},
	schema.Text:     {"kvp_text_st", "KVP_TEXT", "", "", ""},
	schema.Bin:      {"kvp_bin_st", "KVP_BIN", "", "", ""},
	schema.Msg:      {"kvp_msg_st", "KVP_MSG", "", "", ""},
	schema.Bool:     {"kvp_bool_st", "KVP_BOOL", "bool", "int", "kvp_bool"},
	schema.Float64:  {"kvp_float64_st", "KVP_FLOAT64", "float64", "double", "kvp_float64"},
	schema.DS:       {"kvp_ds_st", "KVP_DS", "ds", "int32_t", "kvp_ds"},
	schema.TupleSet: {"kvp_tupleset_st", "KVP_TUPLEDEF", "", "", ""},
	schema.Rec:      {"", "KVP_MSG", "", "", ""},
}

var cKeywords = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true, "const": true, "continue": true,
	"default": true, "do": true, "double": true, "else": true, "enum": true, "extern": true,
	"float": true, "for": true, "goto": true, "if": true, "inline": true, "int": true, "long": true,
	"register": true, "restrict": true, "return": true, "short": true, "signed": true, "sizeof": true,
	"static": true, "struct": true, "switch": true, "typedef": true, "union": true, "unsigned": true,
	"void": true, "volatile": true, "while": true, "kvp": true,
}

var prefixRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// C generates a header and a source over kvp_arena.h. prefix starts every
// name ("api": API_CMD_LOGIN, api_login, api_login_decode); hname is the
// header's file name, included by the source.
//
// Per block it writes a struct of fixed size whose members are records: a
// value and whether it is null, or where the bytes of a text, a list or a
// tupleset lie in a buffer. With it go _put (the fields, in order, into a
// kvp_out), _size and _encode (struct to message), _read and _decode
// (message to struct), and per list field _add_<field> (one more element)
// and _<field> (the walk of a decoded one).
func C(f *schema.File, prefix, hname string) (h, c []byte, err error) {
	if !prefixRe.MatchString(prefix) {
		return nil, nil, fmt.Errorf("C prefix %q: use lowercase letters, digits and _", prefix)
	}
	order, err := cOrder(f)
	if err != nil {
		return nil, nil, err
	}
	tn, err := typeNames(f, schema.C, prefix)
	if err != nil {
		return nil, nil, err
	}
	if err := cCheck(f, prefix, tn); err != nil {
		return nil, nil, err
	}
	P := strings.ToUpper(prefix)
	guard := strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(baseName(hname), "_"))

	var hb, cb strings.Builder
	hw := func(format string, a ...any) { fmt.Fprintf(&hb, format, a...) }
	cw := func(format string, a ...any) { fmt.Fprintf(&cb, format, a...) }

	sv, err := cService(f, prefix, tn)
	if err != nil {
		return nil, nil, err
	}
	hw("/* %s */\n", header(f))
	hw("%s", cUsage)
	if sv != nil {
		hw("%s", cServerUsage)
	}
	hw("#ifndef %s\n#define %s\n\n#include \"kvp_arena.h\"\n\n", guard, guard)
	hw("#ifdef __cplusplus\nextern \"C\" {\n#endif\n\n")
	hw("/* Command and record numbers. */\n")
	for _, bl := range f.Blocks {
		switch bl.Kind {
		case schema.Command:
			hw("#define %s_CMD_%s %du\n", P, strings.ToUpper(bl.Name), bl.Number)
		case schema.Record:
			hw("#define %s_REC_%s %du\n", P, strings.ToUpper(bl.Name), bl.Number)
		}
	}
	for _, bl := range f.Blocks {
		if len(bl.Fields) == 0 {
			continue
		}
		hw("\n/* Keys of %s %s. */\n", bl.Kind, bl.Name)
		for _, fl := range bl.Fields {
			hw("#define %s %d\n", cKey(P, bl, fl), fl.Key)
		}
	}

	cw("/* %s */\n#include \"%s\"\n\n#include <string.h>\n", header(f), baseName(hname))

	// every struct first, so a block may hold a list of a record declared
	// after it; then the functions
	for _, bl := range order {
		cStruct(hw, tn, bl)
	}
	for _, bl := range order {
		cBlock(hw, cw, tn, P, bl)
	}
	if sv != nil {
		cServer(hw, cw, sv, tn, P)
		cClient(hw, cw, sv, tn)
	}
	hw("\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(hb.String()), []byte(cb.String()), nil
}

const cUsage = `
/*
 * Per block X: a struct X of fixed size, the caller's to declare and to set
 * to zero. Its members are records (kvp_arena.h): a scalar is { null, value }
 * and is assigned directly; a text, bin or msg is { null, size, off }, its
 * bytes in an arena -- set it with kvp_set_text or kvp_set_bytes, read it
 * with kvp_st_ptr; a record is { null, has, value }; a list and a tupleset
 * are filled through functions. The arena is a kvp_buffer (dyn_buffer.h)
 * over the application's malloc and free: it holds everything of variable
 * size, and one kvp_buffer_free releases it all.
 *
 *   X_size(&s, &a)                    the exact size of the message
 *   X_encode(&s, &a, buf, cap, &len)  the message, into the caller's buffer
 *   X_decode(&b, &s)                  the struct from the message b holds
 *                                     (kvp_buffer_wrap over the bytes
 *                                     received): KVP_E_MISSING when a
 *                                     required field is absent, KVP_E_REPLY
 *                                     for an error reply (read it with
 *                                     kvp_error_text). Nothing is copied:
 *                                     the struct refers to the bytes of b
 *   X_put(&o, &a, &s), X_read(&b, &m, &s)   the same over a kvp_out and a
 *                                     parsed message that lies in b
 * and per list field F:
 *   X_add_F(&a, &s, v)                one more element, copied into the arena
 *   X_F(&b, &s, &it, &v)              the walk of a decoded struct: zero it
 *                                     first, then call until it returns
 *                                     KVP_E_NOT_FOUND
 * A list of records takes each element as a struct of its own, filled (its
 * lists too) before it is added.
 *
 * What is written follows the declaration: a field always; an optional one
 * when it is not empty (0, no bytes, has 0, a tupleset of no fields); a
 * nullable one always, as a null when its null is set; an optional nullable
 * one when it is not null. Reading mirrors it: an optional field that did
 * not arrive reads empty, an optional nullable one reads null. So a struct
 * at zero writes every field that is not optional, as its empty value: a
 * zero, no bytes, a message of command 0 and no items, a record of empty
 * fields, a tuple definition of no fields.
 *
 * A tupleset field is built with kvp_tupleset_field, kvp_tupleset_begin,
 * kvp_tupleset_int32... and kvp_tupleset_end on the member, and read with
 * kvp_tupleset_open over the struct's kvp member.
 *
 * A decoded struct can be encoded again, with the buffer it was decoded
 * from: its lists and tuplesets are written as they arrived.
 */

`

func cKey(P string, bl *schema.Block, fl *schema.Field) string {
	return P + "_" + strings.ToUpper(blockName(bl)) + "_" + strings.ToUpper(fl.Name)
}

func cNum(P string, bl *schema.Block) string {
	if bl.Kind == schema.Record {
		return P + "_REC_" + strings.ToUpper(bl.Name)
	}
	return P + "_CMD_" + strings.ToUpper(bl.Name)
}

// cFlags is the flags argument a field's declaration gives the kvp_put_
// and kvp_read_ functions.
func cFlags(fl *schema.Field) string {
	switch {
	case fl.Optional && fl.Nullable:
		return "KVP_OPTIONAL | KVP_NULLABLE"
	case fl.Optional:
		return "KVP_OPTIONAL"
	case fl.Nullable:
		return "KVP_NULLABLE"
	}
	return "0"
}

// cOrder puts records before the blocks that embed them (a single record
// field is a struct member by value) and refuses a record that contains
// itself that way.
func cOrder(f *schema.File) ([]*schema.Block, error) {
	var out []*schema.Block
	state := map[*schema.Block]int{} // 1 visiting, 2 done
	var visit func(b *schema.Block) error
	visit = func(b *schema.Block) error {
		switch state[b] {
		case 1:
			return fmt.Errorf("%s:%d: record %s contains itself (C needs a list for that)", f.Name, b.Line, b.Name)
		case 2:
			return nil
		}
		state[b] = 1
		for _, fl := range b.Fields {
			if fl.Type == schema.Rec && !fl.List {
				if err := visit(fl.Record); err != nil {
					return err
				}
			}
		}
		state[b] = 2
		out = append(out, b)
		return nil
	}
	for _, b := range f.Records() {
		if err := visit(b); err != nil {
			return nil, err
		}
	}
	for _, b := range f.Blocks {
		if b.Kind != schema.Record {
			if err := visit(b); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// cCheck refuses names C cannot take and macros that would collide.
func cCheck(f *schema.File, prefix string, tn names) error {
	P := strings.ToUpper(prefix)
	seen := map[string]string{}
	add := func(id, what string) error {
		if o, ok := seen[id]; ok {
			return fmt.Errorf("%s: C name %s of %s collides with %s", f.Name, id, what, o)
		}
		seen[id] = what
		return nil
	}
	for _, bl := range f.Blocks {
		if bl.Kind != schema.Reply {
			if err := add(cNum(P, bl), bl.Kind.String()+" "+bl.Name); err != nil {
				return err
			}
		}
		if err := add(tn[bl], bl.Kind.String()+" "+bl.Name); err != nil {
			return err
		}
		if bl.Kind == schema.Record { // the record of a field of that type
			if err := add(tn[bl]+"_st", "the field record of "+bl.Name); err != nil {
				return err
			}
		}
		for _, fl := range bl.Fields {
			if cKeywords[fl.Name] {
				return fmt.Errorf("%s:%d: field name %q is a C keyword", f.Name, fl.Line, fl.Name)
			}
			if err := add(cKey(P, bl, fl), "field "+bl.Name+"."+fl.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// cStruct writes the struct of a block and, for a record, the record a
// field of that type is.
func cStruct(hw func(string, ...any), tn names, bl *schema.Block) {
	name := tn[bl]
	switch bl.Kind {
	case schema.Command:
		hw("\n/* Request of command %s (%d). */\n", bl.Name, bl.Number)
	case schema.Reply:
		hw("\n/* Reply to command %s (%d). */\n", bl.Name, bl.Number)
	default:
		hw("\n/* Record %s (%d). */\n", bl.Name, bl.Number)
	}
	hw("typedef struct {\n")
	for _, fl := range bl.Fields {
		typ := cTypes[fl.Type].st
		note := ""
		switch {
		case fl.List:
			typ = "kvp_list_st"
			note = fmt.Sprintf(", list: %s_add_%s, %s_%s", name, fl.Name, name, fl.Name)
		case fl.Type == schema.Rec:
			typ = tn[fl.Record] + "_st"
		}
		if fl.Optional {
			note += ", optional"
		}
		if fl.Nullable {
			note += ", nullable"
		}
		if fl.Type == schema.TupleSet {
			note += ": kvp_tupleset_field..., kvp_tupleset_open"
		}
		hw("    %s %s; /* key %d%s */\n", typ, fl.Name, fl.Key, note)
	}
	hw("    kvp_span kvp; /* where the message a decoded struct was read from lies */\n")
	hw("} %s;\n", name)
	if bl.Kind == schema.Record {
		hw("\n/* A field of type %s: has says whether an optional one is there. */\n", bl.Name)
		hw("typedef struct {\n    int null;\n    int has;\n    %s value;\n} %s_st;\n", name, name)
	}
}

// cElemOut is the type a list's walk gives each element as.
func cElemOut(tn names, fl *schema.Field) string {
	switch {
	case fl.Type == schema.Rec:
		return tn[fl.Record]
	case cTypes[fl.Type].plain != "":
		return cTypes[fl.Type].plain
	}
	return cTypes[fl.Type].st
}

// cAddParam is the parameter one element is added to a list as: a scalar
// by value, a text or bin by reference to the caller's bytes, a nested
// message parsed, a record as its struct.
func cAddParam(tn names, fl *schema.Field) string {
	switch fl.Type {
	case schema.Text:
		return "const kvp_utf8 *v"
	case schema.Bin:
		return "const kvp_bytes *v"
	case schema.Msg:
		return "const kvp_message *v"
	case schema.Rec:
		return "const " + tn[fl.Record] + " *v"
	}
	return cTypes[fl.Type].plain + " v"
}

func cBlock(hw, cw func(string, ...any), tn names, P string, bl *schema.Block) {
	name := tn[bl]
	hasRec, hasField := false, false
	for _, fl := range bl.Fields {
		if fl.List {
			continue
		}
		hasField = true
		if fl.Type == schema.Rec {
			hasRec = true
		}
	}

	// prototypes
	hw("\n/* %s */\n", name)
	hw("void %s_put(kvp_out *o, const kvp_buffer *a, const %s *s);\n", name, name)
	hw("size_t %s_size(const %s *s, const kvp_buffer *a);\n", name, name)
	hw("int %s_encode(const %s *s, const kvp_buffer *a, void *buf, size_t cap, size_t *len);\n", name, name)
	hw("int %s_read(const kvp_buffer *b, const kvp_message *m, %s *s);\n", name, name)
	hw("int %s_decode(const kvp_buffer *b, %s *s);\n", name, name)
	for _, fl := range bl.Fields {
		if !fl.List {
			continue
		}
		hw("int %s_add_%s(kvp_buffer *a, %s *s, %s);\n", name, fl.Name, name, cAddParam(tn, fl))
		hw("int %s_%s(const kvp_buffer *b, const %s *s, kvp_item *it, %s *v);\n", name, fl.Name, name, cElemOut(tn, fl))
	}

	// put
	cw("\nvoid %s_put(kvp_out *o, const kvp_buffer *a, const %s *s)\n{\n", name, name)
	if hasRec {
		cw("    kvp_out sub;\n")
	}
	cw("    (void)o;\n    (void)a;\n    (void)s;\n")
	for _, fl := range packOrder(bl) {
		key := cKey(P, bl, fl)
		v := "&s->" + fl.Name
		t := cTypes[fl.Type]
		switch {
		case fl.List:
			cw("    kvp_put_list(o, a, %s, %s, &s->kvp, %s);\n", key, t.kvp, v)
		case fl.Type == schema.TupleSet:
			cw("    kvp_put_tupleset(o, a, %s, %s, &s->kvp, %s);\n", key, cFlags(fl), v)
		case fl.Type == schema.Rec:
			cw("    if (kvp_put_begin(o, %s, %s, s->%s.null, s->%s.has, &sub, %s)) {\n", key, cFlags(fl), fl.Name, fl.Name, cNum(P, fl.Record))
			cw("        %s_put(&sub, a, &s->%s.value);\n        kvp_put_end(o, &sub);\n    }\n", tn[fl.Record], fl.Name)
		case t.fn == "":
			cw("    kvp_put_bytes(o, a, %s, %s, %s, %s);\n", key, t.kvp, cFlags(fl), v)
		default:
			cw("    kvp_put_%s(o, %s, %s, %s);\n", t.fn, key, cFlags(fl), v)
		}
	}
	cw("}\n")

	// size
	cw("\nsize_t %s_size(const %s *s, const kvp_buffer *a)\n{\n", name, name)
	cw("    kvp_out o;\n    kvp_out_measure(&o);\n    %s_put(&o, a, s);\n    return o.size;\n}\n", name)

	// encode
	cw("\nint %s_encode(const %s *s, const kvp_buffer *a, void *buf, size_t cap, size_t *len)\n{\n", name, name)
	cw("    kvp_out o;\n    kvp_out_begin(&o, buf, cap, %s);\n    %s_put(&o, a, s);\n    return kvp_out_end(&o, len);\n}\n", cNum(P, bl), name)

	// read
	cw("\nint %s_read(const kvp_buffer *b, const kvp_message *m, %s *s)\n{\n", name, name)
	if hasRec {
		cw("    kvp_message sub;\n")
	}
	if hasField {
		cw("    int rc;\n")
	}
	cw("    memset(s, 0, sizeof *s);\n    kvp_span_of(b, m, &s->kvp);\n")
	if bl.Kind == schema.Reply {
		cw("    if (kvp_is_error(m))\n        return KVP_E_REPLY;\n")
	}
	for _, fl := range bl.Fields {
		key := cKey(P, bl, fl)
		v := "&s->" + fl.Name
		t := cTypes[fl.Type]
		switch {
		case fl.List:
			cw("    kvp_read_list(m, %s, %s, %s);\n", key, t.kvp, v)
			continue
		case fl.Type == schema.TupleSet:
			cw("    rc = kvp_read_tupleset(m, %s, %s, %s);\n", key, cFlags(fl), v)
		case fl.Type == schema.Rec:
			cw("    rc = kvp_read_rec(m, %s, %s, &s->%s.null, &s->%s.has, &sub);\n", key, cFlags(fl), fl.Name, fl.Name)
			cw("    if (rc == KVP_OK && s->%s.has)\n        rc = %s_read(b, &sub, &s->%s.value);\n", fl.Name, tn[fl.Record], fl.Name)
		case t.fn == "":
			cw("    rc = kvp_read_bytes(b, m, %s, %s, %s, %s);\n", key, t.kvp, cFlags(fl), v)
		default:
			cw("    rc = kvp_read_%s(m, %s, %s, %s);\n", t.fn, key, cFlags(fl), v)
		}
		cw("    if (rc != KVP_OK)\n        return rc;\n")
	}
	cw("    return KVP_OK;\n}\n")

	// decode
	cw("\nint %s_decode(const kvp_buffer *b, %s *s)\n{\n", name, name)
	cw("    kvp_message m;\n    int rc = b ? kvp_parse(b->data, b->size, &m) : KVP_E_ARGUMENT;\n")
	cw("    if (rc != KVP_OK) {\n        memset(s, 0, sizeof *s);\n        return rc;\n    }\n")
	cw("    return %s_read(b, &m, s);\n}\n", name)

	// lists
	for _, fl := range bl.Fields {
		if !fl.List {
			continue
		}
		key := cKey(P, bl, fl)
		t := cTypes[fl.Type]
		l := "&s->" + fl.Name

		cw("\nint %s_add_%s(kvp_buffer *a, %s *s, %s)\n{\n", name, fl.Name, name, cAddParam(tn, fl))
		switch fl.Type {
		case schema.Text, schema.Bin:
			cw("    if (!v)\n        return KVP_E_ARGUMENT;\n")
			cw("    return kvp_list_add(a, %s, %s, v->p, v->len);\n", l, t.kvp)
		case schema.Msg:
			cw("    if (!v || v->len > 0xFFFFFFFFu)\n        return KVP_E_ARGUMENT;\n")
			cw("    return kvp_list_add(a, %s, KVP_MSG, v->buf, (uint32_t)v->len);\n", l)
		case schema.Rec:
			rec := tn[fl.Record]
			// the element is encoded in place, in a node of the arena
			cw("    size_t n = %s_size(v, a), len = 0;\n    kvp_off node;\n    int rc;\n", rec)
			cw("    if (n > 0xFFFFFFFFu)\n        return KVP_E_FULL;\n")
			cw("    rc = kvp_list_reserve(a, KVP_MSG, (uint32_t)n, &node);\n    if (rc != KVP_OK)\n        return rc;\n")
			cw("    rc = %s_encode(v, a, kvp_list_value(a, node), n, &len);\n    if (rc != KVP_OK)\n        return rc;\n", rec)
			cw("    return kvp_list_link(a, %s, node);\n", l)
		default:
			cw("    return kvp_list_add_%s(a, %s, v);\n", t.fn, l)
		}
		cw("}\n")

		cw("\nint %s_%s(const kvp_buffer *b, const %s *s, kvp_item *it, %s *v)\n{\n", name, fl.Name, name, cElemOut(tn, fl))
		cw("    kvp_message m;\n")
		if fl.Type == schema.Rec {
			cw("    kvp_message sub;\n")
		}
		cw("    int rc = kvp_span_message(b, &s->kvp, &m);\n    if (rc != KVP_OK)\n        return rc;\n")
		cw("    for (;;) {\n")
		cw("        rc = it->value ? kvp_find_next(&m, it) : kvp_find(&m, %s, it);\n", key)
		cw("        if (rc != KVP_OK)\n            return rc;\n")
		switch {
		case fl.Type == schema.Rec:
			cw("        if (kvp_msg(it, &sub) == KVP_OK)\n            return %s_read(b, &sub, v);\n", tn[fl.Record])
		case t.fn == "":
			cw("        if (kvp_read_item(b, it, %s, v) == KVP_OK)\n            return KVP_OK;\n", t.kvp)
		default:
			cw("        if (%s(it, v) == KVP_OK)\n            return KVP_OK;\n", t.item)
		}
		cw("    }\n}\n")
	}
}

// cSvc is what the server side of a declaration is called in C.
type cSvc struct {
	f        *schema.File
	client   string            // the struct of a client
	call     map[string]string // per command, the function that calls it
	handler  string            // the struct of functions
	dispatch string            // the multiplexer
	msg      map[string]string // per command, the struct of its message
	reply    map[string]*schema.Block
}

// cService names the server side: the handler struct, the dispatch function
// and, per command, the struct of the message of a call. nil when the
// declaration has no command.
func cService(f *schema.File, prefix string, tn names) (*cSvc, error) {
	if len(f.Commands()) == 0 {
		return nil, nil
	}
	base := f.ServiceName(schema.C, "server")
	if base == "" {
		base = prefix
	}
	sv := &cSvc{f: f, handler: base + "_handler", dispatch: base + "_dispatch", msg: map[string]string{}, reply: map[string]*schema.Block{}}
	taken := map[string]string{}
	for b, n := range tn {
		taken[n] = b.Kind.String() + " " + b.Name
		if b.Kind == schema.Record {
			taken[n+"_st"] = "the field record of " + b.Name
		}
	}
	claim := func(name, what string, line int) error {
		if o, ok := taken[name]; ok {
			return fmt.Errorf("%s:%d: %s is called %s, the name of %s", f.Name, line, what, name, o)
		}
		taken[name] = what
		return nil
	}
	if err := claim(sv.handler, "the handler", 1); err != nil {
		return nil, err
	}
	if err := claim(sv.dispatch, "the multiplexer", 1); err != nil {
		return nil, err
	}
	for _, b := range f.Replies() {
		sv.reply[b.Name] = b
	}
	for _, b := range f.Commands() {
		name := prefix + "_" + b.Name + "_msg"
		if err := claim(name, "the message of command "+b.Name, b.Line); err != nil {
			return nil, err
		}
		sv.msg[b.Name] = name
	}
	cbase := f.ServiceName(schema.C, "client")
	if cbase == "" {
		cbase = prefix
	}
	sv.client, sv.call = cbase+"_client", map[string]string{}
	if err := claim(sv.client, "the client", 1); err != nil {
		return nil, err
	}
	for _, b := range f.Commands() {
		name := cbase + "_call_" + b.Name
		if err := claim(name, "the call of command "+b.Name, b.Line); err != nil {
			return nil, err
		}
		sv.call[b.Name] = name
	}
	return sv, nil
}

const cServerUsage = `/*
 * The server side, with no transport in it. Three pieces:
 *   the handler   a struct with a function per command, on_<command>(app,
 *                 msg, ctx), and app, the application's own pointer
 *   the message   msg, created by the multiplexer for each call:
 *                   msg->request   the request, decoded; its texts lie in
 *                                  msg->b, the buffer the message came in
 *                   msg->reply     the reply, at zero, to fill (there is
 *                                  none when the command declares no reply)
 *                 The reply is written with its own _size and _encode, and
 *                 an arena of the handler's for what it holds of variable
 *                 size
 *   the context   ctx, whatever the caller of the multiplexer gives for the
 *                 call: it is passed on and never looked at
 *
 *   X_dispatch(&handler, app, &b, ctx)   b holds a message (kvp_buffer_wrap)
 *
 * It verifies the message, decodes the request into the message of its
 * command and calls the handler; that is all it does. What is replied, and
 * how it reaches the caller, is the handler's. It calls nobody and returns
 * the error of kvp_parse when the bytes are no message, KVP_E_NOT_FOUND when
 * the command is none of these or its function is NULL, and KVP_E_MISSING
 * when the request misses a required field.
 *
 * The client side, with no transport in it either. A client is a function,
 * call(transport, io), and transport, its own pointer: the function sends
 * the request io holds and leaves the reply in io, returning 0, or anything
 * else when it could not. Per command:
 *
 *   X_call_<command>(&client, &req, &a, &io, &reply)
 *
 * encodes req (its variable data in the arena a) into io, calls, and decodes
 * the reply into reply, which refers to the bytes of io. io is a growing
 * kvp_buffer of the caller's, other than a: what it held is gone. It returns
 * KVP_E_CALL when the transport failed, KVP_E_REPLY when the server replied
 * with an error (read it with kvp_error_text over io), KVP_E_MISSING when
 * the reply misses a required field, KVP_E_FULL when there is no memory. A
 * command that declares no reply takes no reply argument.
 */

`

// cServer writes the message struct of every command, the handler struct
// and the multiplexer.
func cServer(hw, cw func(string, ...any), sv *cSvc, tn names, P string) {
	cmds := sv.f.Commands()
	for _, b := range cmds {
		hw("\n/* The message of a call to command %s (%d). */\ntypedef struct {\n", b.Name, b.Number)
		hw("    %s request;\n", tn[b])
		if r := sv.reply[b.Name]; r != nil {
			hw("    %s reply;\n", tn[r])
		}
		hw("    const kvp_buffer *b; /* the buffer the request lies in */\n} %s;\n", sv.msg[b.Name])
	}
	hw("\n/* The handler: a function per command. */\ntypedef struct {\n")
	for _, b := range cmds {
		hw("    void (*on_%s)(void *app, %s *msg, void *ctx);\n", b.Name, sv.msg[b.Name])
	}
	hw("} %s;\n", sv.handler)
	hw("\n/* The multiplexer. */\nint %s(const %s *h, void *app, const kvp_buffer *b, void *ctx);\n", sv.dispatch, sv.handler)

	// one function per command, so that only the message of the command
	// that arrived takes room on the stack
	for _, b := range cmds {
		cw("\nstatic int %s_%s(const %s *h, void *app, const kvp_buffer *b, const kvp_message *m, void *ctx)\n{\n", sv.dispatch, b.Name, sv.handler)
		cw("    %s msg;\n    int rc;\n    if (!h->on_%s)\n        return KVP_E_NOT_FOUND;\n", sv.msg[b.Name], b.Name)
		cw("    memset(&msg, 0, sizeof msg);\n    msg.b = b;\n")
		cw("    rc = %s_read(b, m, &msg.request);\n    if (rc != KVP_OK)\n        return rc;\n", tn[b])
		cw("    h->on_%s(app, &msg, ctx);\n    return KVP_OK;\n}\n", b.Name)
	}
	cw("\nint %s(const %s *h, void *app, const kvp_buffer *b, void *ctx)\n{\n", sv.dispatch, sv.handler)
	cw("    kvp_message m;\n    int rc;\n    if (!h || !b)\n        return KVP_E_ARGUMENT;\n")
	cw("    rc = kvp_parse(b->data, b->size, &m);\n    if (rc != KVP_OK)\n        return rc;\n    switch (m.command) {\n")
	for _, b := range cmds {
		cw("    case %s:\n        return %s_%s(h, app, b, &m, ctx);\n", cNum(P, b), sv.dispatch, b.Name)
	}
	cw("    default:\n        break;\n    }\n    return KVP_E_NOT_FOUND;\n}\n")
}

// CHandler generates the skeleton of the handler an application writes for
// the server side: a function per command and the struct that holds them,
// to copy and fill. It is no part of what is compiled. hname is the header
// of the generated code. nil when the declaration has no command.
func CHandler(f *schema.File, prefix, hname string) ([]byte, error) {
	tn, err := typeNames(f, schema.C, prefix)
	if err != nil {
		return nil, err
	}
	sv, err := cService(f, prefix, tn)
	if err != nil || sv == nil {
		return nil, err
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	fields := func(bl *schema.Block) string {
		var n []string
		for _, fl := range bl.Fields {
			n = append(n, fl.Name)
		}
		if len(n) == 0 {
			return "no fields"
		}
		return strings.Join(n, ", ")
	}
	w("/*\n * Skeleton of the handler of %s, generated by kvpack: a function per\n", baseName(f.Name))
	w(" * command and the struct that holds them, which an application gives to\n")
	w(" * the multiplexer. Copy it under a name of your own and fill it; this file\n")
	w(" * is written again every time the code is generated.\n *\n")
	w(" *   %s(&my_handler, app, &b, ctx);\n */\n", sv.dispatch)
	w("#include \"%s\"\n", baseName(hname))
	for _, c := range f.Commands() {
		w("\n/* Command %s (%d).\n", c.Name, c.Number)
		w(" *   msg->request   %s: %s\n", tn[c], fields(c))
		if r := sv.reply[c.Name]; r != nil {
			w(" *   msg->reply     %s, at zero, to fill: %s\n", tn[r], fields(r))
		} else {
			w(" *   (the command declares no reply)\n")
		}
		w(" *   msg->b         the buffer the request lies in\n")
		w(" *   app, ctx       the application's own, and what the caller gave for this call */\n")
		w("static void on_%s(void *app, %s *msg, void *ctx)\n{\n    (void)app;\n    (void)msg;\n    (void)ctx;\n}\n", c.Name, sv.msg[c.Name])
	}
	w("\nconst %s my_handler = {\n", sv.handler)
	for i, c := range f.Commands() {
		sep := ","
		if i == len(f.Commands())-1 {
			sep = ""
		}
		w("    on_%s%s\n", c.Name, sep)
	}
	w("};\n")
	return []byte(b.String()), nil
}

// cClient writes the client side: the struct of a client and a function per
// command that calls it.
func cClient(hw, cw func(string, ...any), sv *cSvc, tn names) {
	hw("\n/* A client: what carries its calls. call sends the request io holds and\n")
	hw(" * leaves the reply in io; it returns 0, or anything else when it failed. */\n")
	hw("typedef struct {\n    int (*call)(void *transport, kvp_buffer *io);\n    void *transport;\n} %s;\n\n", sv.client)
	for _, b := range sv.f.Commands() {
		fn, req := sv.call[b.Name], tn[b]
		r := sv.reply[b.Name]
		args := fmt.Sprintf("const %s *c, const %s *req, const kvp_buffer *a, kvp_buffer *io", sv.client, req)
		if r != nil {
			args += fmt.Sprintf(", %s *reply", tn[r])
		}
		hw("int %s(%s);\n", fn, args)
		cw("\nint %s(%s)\n{\n", fn, args)
		if r == nil {
			cw("    kvp_message m;\n")
		}
		cw("    size_t n, len = 0;\n    uint8_t *p;\n    int rc;\n")
		if r != nil {
			cw("    if (!c || !c->call || !req || !io || !reply)\n        return KVP_E_ARGUMENT;\n")
		} else {
			cw("    if (!c || !c->call || !req || !io)\n        return KVP_E_ARGUMENT;\n")
		}
		cw("    n = %s_size(req, a);\n    io->size = 0;\n    p = kvp_buffer_lock_bytes(io, n);\n    if (!p)\n        return KVP_E_FULL;\n", req)
		cw("    rc = %s_encode(req, a, p, n, &len);\n    if (rc != KVP_OK)\n        return rc;\n", req)
		cw("    kvp_buffer_unlock_bytes(io, len);\n    if (c->call(c->transport, io) != 0)\n        return KVP_E_CALL;\n")
		if r != nil {
			cw("    return %s_decode(io, reply);\n}\n", tn[r])
		} else {
			cw("    rc = kvp_parse(io->data, io->size, &m);\n    if (rc != KVP_OK)\n        return rc;\n    return kvp_is_error(&m) ? KVP_E_REPLY : KVP_OK;\n}\n")
		}
	}
}

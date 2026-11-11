package gen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// cTypes maps a base type to the C type of its struct member, the reader
// by key (kvp_get_*) and the reader of a walked item.
var cTypes = map[string]struct{ typ, get, item string }{
	schema.Int32:   {"int32_t", "kvp_get_int32", "kvp_int32"},
	schema.Int64:   {"int64_t", "kvp_get_int64", "kvp_int64"},
	schema.TS:      {"int64_t", "kvp_get_ts", "kvp_ts"},
	schema.Text:    {"kvp_utf8", "kvp_get_utf8", "kvp_item_utf8"},
	schema.Bin:     {"kvp_bytes", "kvp_get_bin", "kvp_bin"},
	schema.Msg:     {"kvp_message", "kvp_get_msg", "kvp_msg"},
	schema.Bool:    {"int", "kvp_get_bool", "kvp_bool"},
	schema.Float64: {"double", "kvp_get_float64", "kvp_float64"},
	schema.DS:      {"int32_t", "kvp_get_ds", "kvp_ds"},
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

// C generates a header and a source over kvpack.h. prefix starts every
// name ("api": API_CMD_LOGIN, api_login, api_login_decode); hname is the
// header's file name, included by the source.
//
// Per block it writes a struct (text and bin by reference into the
// message, a kvp_message member the lists are walked through) and
// _write/_encode (struct to message), _read/_decode (message to struct),
// and per list field _add_<field> (write one element) and _<field> (walk).
func C(f *schema.File, prefix, hname string) (h, c []byte, err error) {
	if !prefixRe.MatchString(prefix) {
		return nil, nil, fmt.Errorf("C prefix %q: use lowercase letters, digits and _", prefix)
	}
	order, err := cOrder(f)
	if err != nil {
		return nil, nil, err
	}
	if err := cCheck(f, prefix); err != nil {
		return nil, nil, err
	}
	P := strings.ToUpper(prefix)
	guard := strings.ToUpper(regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(baseName(hname), "_"))

	var hb, cb strings.Builder
	hw := func(format string, a ...any) { fmt.Fprintf(&hb, format, a...) }
	cw := func(format string, a ...any) { fmt.Fprintf(&cb, format, a...) }

	hw("/* %s */\n", header(f))
	hw("%s", cUsage)
	hw("#ifndef %s\n#define %s\n\n#include \"kvpack.h\"\n\n", guard, guard)
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

	for _, bl := range order {
		cBlock(hw, cw, prefix, P, bl)
	}
	hw("\n#ifdef __cplusplus\n}\n#endif\n\n#endif /* %s */\n", guard)
	return []byte(hb.String()), []byte(cb.String()), nil
}

const cUsage = `
/*
 * Per block X: a struct X, and
 *   X_encode(&s, buf, cap, &len)   the whole message (lists left empty)
 *   X_write(&w, &s)                the single fields, between kvp_begin and
 *                                  kvp_end, so lists can be added after
 *   X_decode(buf, len, &s)         KVP_E_MISSING when a required field is
 *                                  absent; KVP_E_REPLY for an error reply
 *                                  (read it with kvp_error_text)
 *   X_read(&m, &s)                 the same over a parsed message
 * and per list field F:
 *   X_add_F(&w, v)                 one element, after X_write
 *   X_begin_F(&w, &sub, &v)        a record element left open in sub, to
 *                                  add its own lists; close with
 *                                  kvp_end_msg(&w, &sub)
 *   X_F(&s, &it, &v)               the walk: zero it first, then call
 *                                  until it returns KVP_E_NOT_FOUND
 * Text and bin point into the decoded bytes, which must outlive the struct.
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
func cCheck(f *schema.File, prefix string) error {
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
		if err := add(prefix+"_"+blockName(bl), bl.Kind.String()+" "+bl.Name); err != nil {
			return err
		}
		members := map[string]bool{}
		for _, fl := range bl.Fields {
			if cKeywords[fl.Name] {
				return fmt.Errorf("%s:%d: field name %q is a C keyword", f.Name, fl.Line, fl.Name)
			}
			members[fl.Name] = true
			if err := add(cKey(P, bl, fl), "field "+bl.Name+"."+fl.Name); err != nil {
				return err
			}
		}
		for _, fl := range bl.Fields {
			if fl.Type == schema.Rec && fl.Optional && members["has_"+fl.Name] {
				return fmt.Errorf("%s:%d: field has_%s collides with the presence flag of %s", f.Name, fl.Line, fl.Name, fl.Name)
			}
		}
	}
	return nil
}

// cElem is the C type of one list element or single value, as a pointer
// parameter type ("api_event", "kvp_utf8", "int32_t").
func cElem(prefix string, fl *schema.Field) string {
	if fl.Type == schema.Rec {
		return prefix + "_" + fl.Record.Name
	}
	return cTypes[fl.Type].typ
}

func cBlock(hw, cw func(string, ...any), prefix, P string, bl *schema.Block) {
	name := prefix + "_" + blockName(bl)
	hasRec := false
	for _, fl := range bl.Fields {
		if fl.Type == schema.Rec && !fl.List {
			hasRec = true
		}
	}

	// struct
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
		if fl.List {
			hw("    /* %s: list under key %d, walk it with %s_%s */\n", fl.Name, fl.Key, name, fl.Name)
			continue
		}
		note := ""
		if fl.Optional {
			note = ", optional"
		}
		hw("    %s %s; /* key %d%s */\n", cElem(prefix, fl), fl.Name, fl.Key, note)
		if fl.Type == schema.Rec && fl.Optional {
			hw("    int has_%s; /* 1 when %s is present */\n", fl.Name, fl.Name)
		}
	}
	hw("    kvp_message kvp; /* the message read from; values point into it */\n")
	hw("} %s;\n\n", name)

	// prototypes
	hw("int %s_write(kvp_writer *w, const %s *s);\n", name, name)
	hw("int %s_encode(const %s *s, void *buf, size_t cap, size_t *len);\n", name, name)
	hw("int %s_read(const kvp_message *m, %s *s);\n", name, name)
	hw("int %s_decode(const void *buf, size_t len, %s *s);\n", name, name)
	for _, fl := range bl.Fields {
		if !fl.List {
			continue
		}
		hw("int %s_add_%s(kvp_writer *w, %s);\n", name, fl.Name, cAddParam(prefix, fl))
		if fl.Type == schema.Rec && hasLists(fl.Record) {
			hw("int %s_begin_%s(kvp_writer *w, kvp_writer *sub, %s);\n", name, fl.Name, cAddParam(prefix, fl))
		}
		hw("int %s_%s(const %s *s, kvp_item *it, %s *v);\n", name, fl.Name, name, cElem(prefix, fl))
	}

	// write
	cw("\nint %s_write(kvp_writer *w, const %s *s)\n{\n", name, name)
	if hasRec {
		cw("    kvp_writer sub;\n")
	}
	if len(bl.Fields) == 0 || !hasSingle(bl) {
		cw("    (void)s;\n")
	}
	for _, fl := range bl.Fields {
		if fl.List {
			continue
		}
		key := cKey(P, bl, fl)
		v := "s->" + fl.Name
		var stmt, cond string
		switch fl.Type {
		case schema.Int32:
			stmt, cond = fmt.Sprintf("kvp_add_int32(w, %s, %s);", key, v), v+" != 0"
		case schema.Int64:
			stmt, cond = fmt.Sprintf("kvp_add_int64(w, %s, %s);", key, v), v+" != 0"
		case schema.TS:
			stmt, cond = fmt.Sprintf("kvp_add_ts(w, %s, %s);", key, v), v+" != 0"
		case schema.Bool:
			stmt, cond = fmt.Sprintf("kvp_add_bool(w, %s, %s);", key, v), v+" != 0"
		case schema.Float64:
			stmt, cond = fmt.Sprintf("kvp_add_float64(w, %s, %s);", key, v), v+" != 0"
		case schema.DS:
			stmt, cond = fmt.Sprintf("kvp_add_ds(w, %s, %s);", key, v), v+" != 0"
		case schema.Text:
			stmt, cond = fmt.Sprintf("kvp_add_utf8(w, %s, &%s);", key, v), v+".len != 0"
		case schema.Bin:
			stmt, cond = fmt.Sprintf("kvp_add_bin(w, %s, %s.p, %s.len);", key, v, v), v+".len != 0"
		case schema.Msg:
			stmt, cond = fmt.Sprintf("kvp_add_msg(w, %s, %s.buf, %s.len);", key, v, v), v+".buf != NULL"
		case schema.Rec:
			stmt = fmt.Sprintf("kvp_begin_msg(w, %s, &sub, %s);\n        %s_%s_write(&sub, &%s);\n        kvp_end_msg(w, &sub);",
				key, cNum(P, fl.Record), prefix, fl.Record.Name, v)
			cond = "s->has_" + fl.Name
		}
		if fl.Optional || fl.Type == schema.Msg {
			cw("    if (%s) {\n        %s\n    }\n", cond, stmt)
		} else {
			cw("    %s\n", strings.ReplaceAll(stmt, "\n        ", "\n    "))
		}
	}
	cw("    return w->error;\n}\n")

	// encode
	cw("\nint %s_encode(const %s *s, void *buf, size_t cap, size_t *len)\n{\n", name, name)
	cw("    kvp_writer w;\n    kvp_begin(&w, buf, cap, %s);\n    %s_write(&w, s);\n    return kvp_end(&w, len);\n}\n", cNum(P, bl), name)

	// read
	cw("\nint %s_read(const kvp_message *m, %s *s)\n{\n", name, name)
	if hasRec {
		cw("    kvp_message sub;\n    int rc;\n")
	}
	cw("    memset(s, 0, sizeof *s);\n    s->kvp = *m;\n")
	if bl.Kind == schema.Reply {
		cw("    if (kvp_is_error(m))\n        return KVP_E_REPLY;\n")
	}
	for _, fl := range bl.Fields {
		if fl.List {
			continue
		}
		key := cKey(P, bl, fl)
		v := "s->" + fl.Name
		if fl.Type == schema.Rec {
			cw("    if (kvp_get_msg(m, %s, &sub) == KVP_OK) {\n", key)
			cw("        rc = %s_%s_read(&sub, &%s);\n        if (rc != KVP_OK)\n            return rc;\n", prefix, fl.Record.Name, v)
			if fl.Optional {
				cw("        s->has_%s = 1;\n    }\n", fl.Name)
			} else {
				cw("    } else {\n        return KVP_E_MISSING;\n    }\n")
			}
			continue
		}
		call := cGet(fl, key, v)
		if fl.Required() {
			cw("    if (%s != KVP_OK)\n        return KVP_E_MISSING;\n", call)
		} else {
			cw("    (void)%s;\n", call)
		}
	}
	cw("    return KVP_OK;\n}\n")

	// decode
	cw("\nint %s_decode(const void *buf, size_t len, %s *s)\n{\n", name, name)
	cw("    kvp_message m;\n    int rc = kvp_parse(buf, len, &m);\n")
	cw("    if (rc != KVP_OK) {\n        memset(s, 0, sizeof *s);\n        return rc;\n    }\n")
	cw("    return %s_read(&m, s);\n}\n", name)

	// lists
	for _, fl := range bl.Fields {
		if !fl.List {
			continue
		}
		key := cKey(P, bl, fl)
		cw("\nint %s_add_%s(kvp_writer *w, %s)\n{\n", name, fl.Name, cAddParam(prefix, fl))
		switch fl.Type {
		case schema.Int32:
			cw("    return kvp_add_int32(w, %s, v);\n", key)
		case schema.Int64:
			cw("    return kvp_add_int64(w, %s, v);\n", key)
		case schema.TS:
			cw("    return kvp_add_ts(w, %s, v);\n", key)
		case schema.Bool:
			cw("    return kvp_add_bool(w, %s, v);\n", key)
		case schema.Float64:
			cw("    return kvp_add_float64(w, %s, v);\n", key)
		case schema.DS:
			cw("    return kvp_add_ds(w, %s, v);\n", key)
		case schema.Text:
			cw("    return kvp_add_utf8(w, %s, v);\n", key)
		case schema.Bin:
			cw("    return kvp_add_bin(w, %s, v->p, v->len);\n", key)
		case schema.Msg:
			cw("    return kvp_add_msg(w, %s, v->buf, v->len);\n", key)
		case schema.Rec:
			cw("    kvp_writer sub;\n    kvp_begin_msg(w, %s, &sub, %s);\n    %s_%s_write(&sub, v);\n    return kvp_end_msg(w, &sub);\n",
				key, cNum(P, fl.Record), prefix, fl.Record.Name)
		}
		cw("}\n")
		if fl.Type == schema.Rec && hasLists(fl.Record) {
			cw("\nint %s_begin_%s(kvp_writer *w, kvp_writer *sub, %s)\n{\n", name, fl.Name, cAddParam(prefix, fl))
			cw("    kvp_begin_msg(w, %s, sub, %s);\n    return %s_%s_write(sub, v);\n}\n",
				key, cNum(P, fl.Record), prefix, fl.Record.Name)
		}

		cw("\nint %s_%s(const %s *s, kvp_item *it, %s *v)\n{\n", name, fl.Name, name, cElem(prefix, fl))
		if fl.Type == schema.Rec {
			cw("    kvp_message sub;\n")
		}
		cw("    int rc;\n    for (;;) {\n")
		cw("        rc = it->value ? kvp_find_next(&s->kvp, it) : kvp_find(&s->kvp, %s, it);\n", key)
		cw("        if (rc != KVP_OK)\n            return rc;\n")
		switch fl.Type {
		case schema.Rec:
			cw("        if (kvp_msg(it, &sub) == KVP_OK)\n            return %s_%s_read(&sub, v);\n", prefix, fl.Record.Name)
		case schema.Bin:
			cw("        if (kvp_bin(it, &v->p, &v->len) == KVP_OK)\n            return KVP_OK;\n")
		default:
			cw("        if (%s(it, v) == KVP_OK)\n            return KVP_OK;\n", cTypes[fl.Type].item)
		}
		cw("    }\n}\n")
	}
}

func cAddParam(prefix string, fl *schema.Field) string {
	switch fl.Type {
	case schema.Int32, schema.Int64, schema.TS, schema.Bool, schema.Float64, schema.DS:
		return cTypes[fl.Type].typ + " v"
	}
	return "const " + cElem(prefix, fl) + " *v"
}

func cGet(fl *schema.Field, key, v string) string {
	if fl.Type == schema.Bin {
		return fmt.Sprintf("kvp_get_bin(m, %s, &%s.p, &%s.len)", key, v, v)
	}
	return fmt.Sprintf("%s(m, %s, &%s)", cTypes[fl.Type].get, key, v)
}

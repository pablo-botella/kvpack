package gen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// goTypes maps a base type to its Go type, Add method and getter.
var goTypes = map[string]struct{ typ, add, get, zero string }{
	schema.Int32:    {"int32", "AddInt32", "Int32", "%s != 0"},
	schema.Int64:    {"int64", "AddInt64", "Int64", "%s != 0"},
	schema.TS:       {"time.Time", "AddTime", "Time", "!%s.IsZero()"},
	schema.Text:     {"string", "AddText", "Text", `%s != ""`},
	schema.Bin:      {"[]byte", "AddBin", "Bin", "len(%s) != 0"},
	schema.Msg:      {"*kvpack.Message", "AddMessage", "Message", "%s != nil"},
	schema.Bool:     {"bool", "AddBool", "Bool", "%s"},
	schema.Float64:  {"float64", "AddFloat64", "Float64", "%s != 0"},
	schema.DS:       {"time.Time", "AddDate", "Date", "!%s.IsZero()"},
	schema.TupleSet: {"*kvpack.TupleSet", "AddTupleSet", "TupleSet", "%s != nil"},
}

// Go generates a Go file in package pkg: a constant per command and record
// number, and per block a struct with Pack (struct to message) and Unpack
// (message to struct).
func Go(f *schema.File, pkg string) ([]byte, error) {
	if err := holdsItself(f); err != nil {
		return nil, err
	}
	tn, err := typeNames(f, schema.Go, "")
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	sv, err := goService(f, tn)
	if err != nil {
		return nil, err
	}
	var std []string
	if sv != nil {
		std = append(std, `"context"`)
	}
	if needsErrors(f) || sv != nil { // a call with no reply still reads an error reply
		std = append(std, `"errors"`)
	}
	if needsFmt(f) || sv != nil {
		std = append(std, `"fmt"`)
	}
	if hasType(f, schema.TS) || hasType(f, schema.DS) {
		std = append(std, `"time"`)
	}

	w("// %s\n\n", header(f))
	w("package %s\n\nimport (\n", pkg)
	for _, imp := range std {
		w("\t%s\n", imp)
	}
	if len(std) > 0 {
		w("\n")
	}
	w("\t\"github.com/pablo-botella/kvpack\"\n)\n\n")

	w("// Command and record numbers.\nconst (\n")
	for _, bl := range f.Blocks {
		switch bl.Kind {
		case schema.Command:
			w("\tCmd%s uint32 = %d\n", camel(bl.Name), bl.Number)
		case schema.Record:
			w("\tRec%s uint32 = %d\n", camel(bl.Name), bl.Number)
		}
	}
	w(")\n")

	for _, bl := range f.Blocks {
		goBlock(w, tn, bl)
	}
	if sv != nil {
		goServer(w, sv, tn)
		goClient(w, sv, tn)
	}
	return format.Source([]byte(b.String()))
}

func numConst(bl *schema.Block) string {
	if bl.Kind == schema.Record {
		return "Rec" + camel(bl.Name)
	}
	return "Cmd" + camel(bl.Name)
}

func goFieldType(tn names, fl *schema.Field) string {
	t := goTypes[fl.Type].typ
	if fl.Type == schema.Rec {
		t = tn.camel(fl.Record)
		if !fl.List {
			t = "*" + t
		}
	}
	if fl.List {
		t = "[]" + t
	}
	return t
}

func goBlock(w func(string, ...any), tn names, bl *schema.Block) {
	typ := tn.camel(bl)
	name := blockName(bl)
	switch bl.Kind {
	case schema.Command:
		w("\n// %s is the request of command %s (%d).\n", typ, bl.Name, bl.Number)
	case schema.Reply:
		w("\n// %s is the reply to command %s (%d).\n", typ, bl.Name, bl.Number)
	default:
		w("\n// %s is record %s (%d).\n", typ, bl.Name, bl.Number)
	}
	w("type %s struct {\n", typ)
	for _, fl := range bl.Fields {
		note := ""
		if fl.List {
			note = ", list"
		} else if fl.Optional {
			note = ", optional"
		}
		if fl.Type == schema.TupleSet {
			note += ": a tuple definition and its tuples"
		}
		w("\t%s %s // key %d%s\n", camel(fl.Name), goFieldType(tn, fl), fl.Key, note)
		if fl.Nullable && fl.Optional {
			w("\t%sNull bool // key %d is null: not sent, and what an absent one reads as\n", camel(fl.Name), fl.Key)
		} else if fl.Nullable {
			w("\t%sNull bool // key %d arrives, or is sent, as a null\n", camel(fl.Name), fl.Key)
		}
	}
	w("}\n")

	// Pack
	w("\n// Pack builds the message. Optional fields at their zero value are left out,\n// and what always travels goes as its empty value when left at nil")
	if hasNullable(bl) {
		w(";\n// a nullable field whose Null flag is set goes as a null, whatever its value,\n// and is left out when it is optional too")
	}
	w(".\n")
	w("func (s *%s) Pack() *kvpack.Message {\n\tm := kvpack.New(%s)\n", typ, numConst(bl))
	for _, fl := range packOrder(bl) {
		v := "s." + camel(fl.Name)
		if fl.Nullable && fl.Optional {
			// sent when it is not null, whatever its value
			w("\tif !%sNull {\n", v)
			goAdd(w, tn, fl, v)
			w("\t}\n")
			continue
		}
		if fl.Nullable {
			w("\tif %sNull {\n\t\tm.AddNull(%d)\n\t} else {\n", v, fl.Key)
		}
		switch {
		case fl.List && fl.Type == schema.Rec:
			w("\tfor i := range %s {\n\t\tm.AddMessage(%d, %s[i].Pack())\n\t}\n", v, fl.Key, v)
		case fl.List:
			w("\tfor _, v := range %s {\n\t\tm.%s(%d, v)\n\t}\n", v, goTypes[fl.Type].add, fl.Key)
		case fl.Optional && fl.Type == schema.Rec:
			w("\tif %s != nil {\n\t\tm.AddMessage(%d, %s.Pack())\n\t}\n", v, fl.Key, v)
		case fl.Optional && fl.Type == schema.TupleSet: // no fields is the empty one
			w("\tif %s != nil && %s.Def != nil && len(%s.Def.Fields) != 0 {\n\t\tm.AddTupleSet(%d, %s)\n\t}\n", v, v, v, fl.Key, v)
		case fl.Optional:
			t := goTypes[fl.Type]
			w("\tif "+t.zero+" {\n\t\tm.%s(%d, %s)\n\t}\n", v, t.add, fl.Key, v)
		default:
			goAdd(w, tn, fl, v)
		}
		if fl.Nullable {
			w("\t}\n")
		}
	}
	w("\treturn m\n}\n")

	// Unpack
	w("\n// Unpack fills s from m. It fails when a required field is missing or of\n// another type")
	if bl.Kind == schema.Reply {
		w(", and with the server's text when m is an error reply")
	}
	w(".\n")
	w("func (s *%s) Unpack(m *kvpack.Message) error {\n\t*s = %s{}\n", typ, typ)
	if bl.Kind == schema.Reply {
		w("\tif text, bad := m.ErrorText(); bad {\n\t\treturn errors.New(text)\n\t}\n")
	}
	needOk := false
	for _, fl := range bl.Fields {
		if fl.Required() && fl.Type != schema.Rec {
			needOk = true
		}
	}
	if needOk {
		w("\tvar ok bool\n")
	}
	for _, fl := range bl.Fields {
		v := "s." + camel(fl.Name)
		missing := fmt.Sprintf("errors.New(%q)", name+": missing "+fl.Name)
		if fl.Nullable && fl.Optional {
			// absent, a null or another type: null
			if fl.Type == schema.Rec {
				w("\tif sub, ok := m.Message(%d); ok {\n", fl.Key)
				w("\t\t%s = new(%s)\n", v, tn.camel(fl.Record))
				w("\t\tif err := %s.Unpack(sub); err != nil {\n\t\t\treturn fmt.Errorf(\"%s: %s: %%w\", err)\n\t\t}\n", v, name, fl.Name)
				w("\t} else {\n\t\t%sNull = true\n\t}\n", v)
			} else {
				w("\tif v, ok := m.%s(%d); ok {\n\t\t%s = v\n\t} else {\n\t\t%sNull = true\n\t}\n", goTypes[fl.Type].get, fl.Key, v, v)
			}
			continue
		}
		if fl.Nullable {
			w("\tif m.IsNull(%d) {\n\t\t%sNull = true\n\t} else {\n", fl.Key, v)
		}
		switch {
		case fl.List && fl.Type == schema.Rec:
			w("\tfor _, it := range m.FindAll(%d) {\n", fl.Key)
			w("\t\tsub, ok := it.Message()\n\t\tif !ok {\n\t\t\tcontinue\n\t\t}\n")
			w("\t\tvar r %s\n", tn.camel(fl.Record))
			w("\t\tif err := r.Unpack(sub); err != nil {\n\t\t\treturn fmt.Errorf(\"%s: %s: %%w\", err)\n\t\t}\n", name, fl.Name)
			w("\t\t%s = append(%s, r)\n\t}\n", v, v)
		case fl.List:
			w("\tfor _, it := range m.FindAll(%d) {\n", fl.Key)
			w("\t\tif v, ok := it.%s(); ok {\n\t\t\t%s = append(%s, v)\n\t\t}\n\t}\n", goTypes[fl.Type].get, v, v)
		case fl.Type == schema.Rec:
			w("\tif sub, ok := m.Message(%d); ok {\n", fl.Key)
			w("\t\t%s = new(%s)\n", v, tn.camel(fl.Record))
			w("\t\tif err := %s.Unpack(sub); err != nil {\n\t\t\treturn fmt.Errorf(\"%s: %s: %%w\", err)\n\t\t}\n", v, name, fl.Name)
			if fl.Required() {
				w("\t} else {\n\t\treturn %s\n", missing)
			}
			w("\t}\n")
		case fl.Required():
			w("\tif %s, ok = m.%s(%d); !ok {\n\t\treturn %s\n\t}\n", v, goTypes[fl.Type].get, fl.Key, missing)
		default:
			w("\t%s, _ = m.%s(%d)\n", v, goTypes[fl.Type].get, fl.Key)
		}
		if fl.Nullable {
			w("\t}\n")
		}
	}
	w("\treturn nil\n}\n")
}

// goAdd writes a field that always travels. A message, a record or a
// tupleset left at nil is the empty one: a message of command 0 and no
// items, a record of empty fields, a definition of no fields.
func goAdd(w func(string, ...any), tn names, fl *schema.Field, v string) {
	switch fl.Type {
	case schema.Rec:
		w("\tif %s != nil {\n\t\tm.AddMessage(%d, %s.Pack())\n\t} else {\n\t\tm.AddMessage(%d, (&%s{}).Pack())\n\t}\n",
			v, fl.Key, v, fl.Key, tn.camel(fl.Record))
	case schema.Msg:
		w("\tif %s != nil {\n\t\tm.AddMessage(%d, %s)\n\t} else {\n\t\tm.AddMessage(%d, kvpack.New(0))\n\t}\n", v, fl.Key, v, fl.Key)
	case schema.TupleSet:
		w("\tif %s != nil && %s.Def != nil {\n\t\tm.AddTupleSet(%d, %s)\n\t} else {\n\t\tm.AddTupleSet(%d, kvpack.NewTupleSet(kvpack.NewTupleDef()))\n\t}\n",
			v, v, fl.Key, v, fl.Key)
	default:
		w("\tm.%s(%d, %s)\n", goTypes[fl.Type].add, fl.Key, v)
	}
}

// hasNullable reports whether a block declares a nullable field.
func hasNullable(bl *schema.Block) bool {
	for _, fl := range bl.Fields {
		if fl.Nullable {
			return true
		}
	}
	return false
}

// needsFmt: record fields wrap the errors of their own Unpack.

func needsFmt(f *schema.File) bool { return hasType(f, schema.Rec) }

// needsErrors: a required field can be missing, a reply can be an error.
func needsErrors(f *schema.File) bool {
	if len(f.Replies()) > 0 {
		return true
	}
	for _, bl := range f.Blocks {
		for _, fl := range bl.Fields {
			if fl.Required() {
				return true
			}
		}
	}
	return false
}

func hasType(f *schema.File, t string) bool {
	for _, bl := range f.Blocks {
		for _, fl := range bl.Fields {
			if fl.Type == t {
				return true
			}
		}
	}
	return false
}

// goSvc is what the server side of a declaration is called in Go.
type goSvc struct {
	f       *schema.File
	client  string            // the client object
	caller  string            // the interface of what carries its calls
	server  string            // the multiplexer
	handler string            // the interface an application implements
	msg     map[string]string // per command, the type of its message
	reply   map[string]*schema.Block
}

// goService names the server side: the multiplexer, the handler interface
// and, per command, the type of the message of a call. nil when the
// declaration has no command.
func goService(f *schema.File, tn names) (*goSvc, error) {
	if len(f.Commands()) == 0 {
		return nil, nil
	}
	sv := &goSvc{f: f, server: "Server", handler: "Handler", msg: map[string]string{}, reply: map[string]*schema.Block{}}
	if n := f.ServiceName(schema.Go, "server"); n != "" {
		sv.server = camel(n)
		sv.handler = sv.server + "Handler"
	}
	taken := map[string]string{}
	for b, n := range tn {
		taken[camel(n)] = b.Kind.String() + " " + b.Name
	}
	claim := func(name, what string, line int) error {
		if o, ok := taken[name]; ok {
			return fmt.Errorf("%s:%d: %s is called %s, the name of %s", f.Name, line, what, name, o)
		}
		taken[name] = what
		return nil
	}
	if err := claim(sv.server, "the multiplexer", 1); err != nil {
		return nil, err
	}
	if err := claim("New"+sv.server, "the constructor of the multiplexer", 1); err != nil {
		return nil, err
	}
	if err := claim(sv.handler, "the handler interface", 1); err != nil {
		return nil, err
	}
	sv.client, sv.caller = "Client", "Caller"
	if n := f.ServiceName(schema.Go, "client"); n != "" {
		sv.client = camel(n)
		sv.caller = sv.client + "Caller"
	}
	if err := claim(sv.client, "the client", 1); err != nil {
		return nil, err
	}
	if err := claim("New"+sv.client, "the constructor of the client", 1); err != nil {
		return nil, err
	}
	if err := claim(sv.caller, "the caller interface", 1); err != nil {
		return nil, err
	}
	for _, b := range f.Replies() {
		sv.reply[b.Name] = b
	}
	for _, b := range f.Commands() {
		name := camel(b.Name) + "Msg"
		if err := claim(name, "the message of command "+b.Name, b.Line); err != nil {
			return nil, err
		}
		sv.msg[b.Name] = name
	}
	return sv, nil
}

// goServer writes the message type of every command, the handler interface
// and the multiplexer.
func goServer(w func(string, ...any), sv *goSvc, tn names) {
	cmds := sv.f.Commands()
	what := "the commands of " + baseName(sv.f.Name)
	if sv.f.Service != "" {
		what = "service " + sv.f.Service
	}
	w("\n// The server side, with no transport in it. An application implements %s,\n", sv.handler)
	w("// a method per command, and gives it to a %s; whoever receives a message\n", sv.server)
	w("// calls Dispatch with it and with the context of that call, which only\n")
	w("// travels through. Dispatch creates the message of the command, unpacks the\n")
	w("// request into it and calls the handler: what is replied, and how it\n")
	w("// reaches the caller, is the handler's, with the message and the context.\n")
	for _, b := range cmds {
		name := sv.msg[b.Name]
		r := sv.reply[b.Name]
		if r != nil {
			w("\n// %s is the message of a call to command %s (%d): the request, unpacked,\n// and its reply, empty, to fill.\n", name, b.Name, b.Number)
			w("type %s struct {\n\tRequest %s\n\tReply %s\n}\n", name, tn.camel(b), tn.camel(r))
			w("\n// Pack returns the reply as a message.\nfunc (m *%s) Pack() *kvpack.Message { return m.Reply.Pack() }\n", name)
		} else {
			w("\n// %s is the message of a call to command %s (%d): the request, unpacked.\n// The command declares no reply.\n", name, b.Name, b.Number)
			w("type %s struct {\n\tRequest %s\n}\n", name, tn.camel(b))
			w("\n// Pack returns the reply as a message: the command's, with no items.\nfunc (m *%s) Pack() *kvpack.Message { return kvpack.New(%s) }\n", name, numConst(b))
		}
		w("\n// PackError returns an error reply instead: the text under key 0.\n")
		w("func (m *%s) PackError(text string) *kvpack.Message { return kvpack.NewError(%s, text) }\n", name, numConst(b))
	}

	w("\n// %s is what an application gives the multiplexer: a method per command.\n", sv.handler)
	w("type %s interface {\n", sv.handler)
	for _, b := range cmds {
		w("\tOn%s(ctx context.Context, msg *%s)\n", camel(b.Name), sv.msg[b.Name])
	}
	w("}\n")
	w("\n// %s is the multiplexer of %s.\ntype %s struct {\n\tHandler %s\n}\n", sv.server, what, sv.server, sv.handler)
	w("\n// New%s returns a multiplexer that calls h.\nfunc New%s(h %s) *%s { return &%s{Handler: h} }\n", sv.server, sv.server, sv.handler, sv.server, sv.server)
	w("\n// Dispatch hands the call m holds to the handler. It fails, calling nobody,\n")
	w("// when the command is none of these or the request misses a required field.\n")
	w("func (s *%s) Dispatch(ctx context.Context, m *kvpack.Message) error {\n\tswitch m.Command {\n", sv.server)
	for _, b := range cmds {
		w("\tcase %s:\n\t\tmsg := new(%s)\n", numConst(b), sv.msg[b.Name])
		w("\t\tif err := msg.Request.Unpack(m); err != nil {\n\t\t\treturn err\n\t\t}\n")
		w("\t\ts.Handler.On%s(ctx, msg)\n", camel(b.Name))
	}
	w("\tdefault:\n\t\treturn fmt.Errorf(\"unknown command %%d\", m.Command)\n\t}\n\treturn nil\n}\n")
}

// GoHandler generates the skeleton of the handler an application writes
// for the server side: a type with a method per command, to copy and fill.
// It is no part of what is compiled. nil when the declaration has no
// command.
func GoHandler(f *schema.File, pkg string) ([]byte, error) {
	tn, err := typeNames(f, schema.Go, "")
	if err != nil {
		return nil, err
	}
	sv, err := goService(f, tn)
	if err != nil || sv == nil {
		return nil, err
	}
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	fields := func(bl *schema.Block) string {
		var n []string
		for _, fl := range bl.Fields {
			n = append(n, camel(fl.Name))
		}
		if len(n) == 0 {
			return "no fields"
		}
		return strings.Join(n, ", ")
	}
	w("// Skeleton of the handler of %s, generated by kvpack: what an\n", baseName(f.Name))
	w("// application gives to the multiplexer, with a method per command. Copy it\n")
	w("// under a name of your own and fill it; this file is written again every\n")
	w("// time the code is generated.\n//\n")
	w("// It is written as part of package %s. From another package, import that\n", pkg)
	w("// one and put its name before the types.\n//\n")
	w("//\tsrv := New%s(&MyHandler{})\n//\terr := srv.Dispatch(ctx, m)\n\n", sv.server)
	w("package %s\n\nimport \"context\"\n\n", pkg)
	w("// MyHandler serves the commands.\ntype MyHandler struct{}\n")
	for _, c := range f.Commands() {
		w("\n// On%s serves command %s (%d).\n//\n", camel(c.Name), c.Name, c.Number)
		w("//\tmsg.Request  %s: %s\n", tn.camel(c), fields(c))
		if r := sv.reply[c.Name]; r != nil {
			w("//\tmsg.Reply    %s, empty, to fill: %s\n", tn.camel(r), fields(r))
		} else {
			w("//\t(the command declares no reply)\n")
		}
		w("//\tmsg.Pack()   the reply as a message\n//\tctx          what the caller of Dispatch gave for this call\n")
		w("func (h *MyHandler) On%s(ctx context.Context, msg *%s) {\n}\n", camel(c.Name), sv.msg[c.Name])
	}
	return format.Source([]byte(b.String()))
}

// goClient writes the client side: the interface of what carries a call and
// a type with a method per command.
func goClient(w func(string, ...any), sv *goSvc, tn names) {
	w("\n// The client side, with no transport in it either: a %s has a method per\n", sv.client)
	w("// command, which packs the request, hands it to a %s and unpacks the\n// reply.\n", sv.caller)
	w("\n// %s is what carries a call: it sends a message and returns the reply\n// to it. A transport is one.\n", sv.caller)
	w("type %s interface {\n\tCall(ctx context.Context, req *kvpack.Message) (*kvpack.Message, error)\n}\n", sv.caller)
	w("\n// %s makes the calls, over a %s.\ntype %s struct {\n\tcaller %s\n}\n", sv.client, sv.caller, sv.client, sv.caller)
	w("\n// New%s returns a client that calls through c.\nfunc New%s(c %s) *%s { return &%s{caller: c} }\n", sv.client, sv.client, sv.caller, sv.client, sv.client)
	for _, b := range sv.f.Commands() {
		name, req := camel(b.Name), tn.camel(b)
		if r := sv.reply[b.Name]; r != nil {
			w("\n// %s calls command %s (%d). The error is the caller's, the one of a\n// reply that misses a required field, or the server's own text when it\n// replied with an error.\n", name, b.Name, b.Number)
			w("func (c *%s) %s(ctx context.Context, req *%s) (*%s, error) {\n", sv.client, name, req, tn.camel(r))
			w("\tm, err := c.caller.Call(ctx, req.Pack())\n\tif err != nil {\n\t\treturn nil, err\n\t}\n")
			w("\treply := new(%s)\n\tif err := reply.Unpack(m); err != nil {\n\t\treturn nil, err\n\t}\n\treturn reply, nil\n}\n", tn.camel(r))
			continue
		}
		w("\n// %s calls command %s (%d), which declares no reply. The error is the\n// caller's, or the server's own text when it replied with an error.\n", name, b.Name, b.Number)
		w("func (c *%s) %s(ctx context.Context, req *%s) error {\n", sv.client, name, req)
		w("\tm, err := c.caller.Call(ctx, req.Pack())\n\tif err != nil {\n\t\treturn err\n\t}\n")
		w("\tif text, bad := m.ErrorText(); bad {\n\t\treturn errors.New(text)\n\t}\n\treturn nil\n}\n")
	}
}

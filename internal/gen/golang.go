package gen

import (
	"fmt"
	"go/format"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// goTypes maps a base type to its Go type, Add method and getter.
var goTypes = map[string]struct{ typ, add, get, zero string }{
	schema.Int32:   {"int32", "AddInt32", "Int32", "%s != 0"},
	schema.Int64:   {"int64", "AddInt64", "Int64", "%s != 0"},
	schema.TS:      {"time.Time", "AddTime", "Time", "!%s.IsZero()"},
	schema.Text:    {"string", "AddText", "Text", `%s != ""`},
	schema.Bin:     {"[]byte", "AddBin", "Bin", "len(%s) != 0"},
	schema.Msg:     {"*kvpack.Message", "AddMessage", "Message", "%s != nil"},
	schema.Bool:    {"bool", "AddBool", "Bool", "%s"},
	schema.Float64: {"float64", "AddFloat64", "Float64", "%s != 0"},
	schema.DS:      {"time.Time", "AddDate", "Date", "!%s.IsZero()"},
}

// Go generates a Go file in package pkg: a constant per command and record
// number, and per block a struct with Pack (struct to message) and Unpack
// (message to struct).
func Go(f *schema.File, pkg string) ([]byte, error) {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	var std []string
	if needsErrors(f) {
		std = append(std, `"errors"`)
	}
	if needsFmt(f) {
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
		goBlock(w, bl)
	}
	return format.Source([]byte(b.String()))
}

func numConst(bl *schema.Block) string {
	if bl.Kind == schema.Record {
		return "Rec" + camel(bl.Name)
	}
	return "Cmd" + camel(bl.Name)
}

func goFieldType(fl *schema.Field) string {
	t := goTypes[fl.Type].typ
	if fl.Type == schema.Rec {
		t = camel(fl.Record.Name)
		if !fl.List {
			t = "*" + t
		}
	}
	if fl.List {
		t = "[]" + t
	}
	return t
}

func goBlock(w func(string, ...any), bl *schema.Block) {
	typ := camel(blockName(bl))
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
		w("\t%s %s // key %d%s\n", camel(fl.Name), goFieldType(fl), fl.Key, note)
	}
	w("}\n")

	// Pack
	w("\n// Pack builds the message. Optional fields at their zero value are left out.\n")
	w("func (s *%s) Pack() *kvpack.Message {\n\tm := kvpack.New(%s)\n", typ, numConst(bl))
	for _, fl := range packOrder(bl) {
		v := "s." + camel(fl.Name)
		switch {
		case fl.List && fl.Type == schema.Rec:
			w("\tfor i := range %s {\n\t\tm.AddMessage(%d, %s[i].Pack())\n\t}\n", v, fl.Key, v)
		case fl.List:
			w("\tfor _, v := range %s {\n\t\tm.%s(%d, v)\n\t}\n", v, goTypes[fl.Type].add, fl.Key)
		case fl.Type == schema.Rec:
			w("\tif %s != nil {\n\t\tm.AddMessage(%d, %s.Pack())\n\t}\n", v, fl.Key, v)
		case fl.Optional || fl.Type == schema.Msg:
			t := goTypes[fl.Type]
			w("\tif "+t.zero+" {\n\t\tm.%s(%d, %s)\n\t}\n", v, t.add, fl.Key, v)
		default:
			w("\tm.%s(%d, %s)\n", goTypes[fl.Type].add, fl.Key, v)
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
		switch {
		case fl.List && fl.Type == schema.Rec:
			w("\tfor _, it := range m.FindAll(%d) {\n", fl.Key)
			w("\t\tsub, ok := it.Message()\n\t\tif !ok {\n\t\t\tcontinue\n\t\t}\n")
			w("\t\tvar r %s\n", camel(fl.Record.Name))
			w("\t\tif err := r.Unpack(sub); err != nil {\n\t\t\treturn fmt.Errorf(\"%s: %s: %%w\", err)\n\t\t}\n", name, fl.Name)
			w("\t\t%s = append(%s, r)\n\t}\n", v, v)
		case fl.List:
			w("\tfor _, it := range m.FindAll(%d) {\n", fl.Key)
			w("\t\tif v, ok := it.%s(); ok {\n\t\t\t%s = append(%s, v)\n\t\t}\n\t}\n", goTypes[fl.Type].get, v, v)
		case fl.Type == schema.Rec:
			w("\tif sub, ok := m.Message(%d); ok {\n", fl.Key)
			w("\t\t%s = new(%s)\n", v, camel(fl.Record.Name))
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
	}
	w("\treturn nil\n}\n")
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

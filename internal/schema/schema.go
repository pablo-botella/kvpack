// Package schema reads a .kvp declaration: the commands, replies and records
// of an application, each a list of named, typed, numbered fields. It is
// the front end of kvpgen; the generators work on the File it returns.
//
// The language is one declaration per line, # starts a comment:
//
//	command login = 1
//	    user      text   = 1
//	    when      ts     = 3   optional
//	reply login
//	    name      text   = 1
//	    events    event  = 3   list
//	record event = 100
//	    id        int32  = 1
//
// A header (command, reply, record) opens a block and the fields that
// follow belong to it; indentation is only for the eye. A field is
// "name type = key [list | optional] [nullable]". A reply takes the number
// of its command. Fields are required unless marked optional; a list may be
// empty. A nullable field may arrive as a null instead of a value. The type
// tupleset is a tuple definition and its tuples, whose shape is only known
// when the message is built.
//
// Two declarations stand outside the blocks:
//
//	service sqlbk
//	include "sqlbk_types.kvp"
//
// service names the service a file declares: its commands grouped under a
// name, which is what the generated names start from. include reads another
// file (its path relative to the one that includes it) into the same
// declaration: names and numbers are checked over the whole, and a type may
// be declared in any of the files. A file that declares a service is a
// declaration on its own and cannot be included.
//
// A names section says what an implementation calls what is generated:
//
//	names                              # every language
//	    record  result = sqlbk_record
//	names c                            # C alone
//	    command *      = call_sqlbk_*
//	names go server                    # one side of one language
//	    service        = sqlbk_service
//
// The header is "names [lang [side]]", lang being go, c, harbour or *, and
// side server, client or *; what is not written is any, and the most
// specific section wins. Inside, "command name", "reply name" and "record
// name" name the type of a block, * standing for all of them and, in the
// name, for each one's own; "package" (Go), "prefix" (C and Harbour) and
// "service" name the rest. A type is named per language, in a section with
// no side: both sides of a language share its types.
package schema

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind tells what a block declares.
type Kind int

const (
	Command Kind = iota // a request
	Reply               // the reply to a command, same number
	Record              // a message nested as a value
)

func (k Kind) String() string {
	switch k {
	case Command:
		return "command"
	case Reply:
		return "reply"
	}
	return "record"
}

// Base types of a field. A field whose type names a record has Type Rec
// and Record set.
const (
	Int32   = "int32"
	Int64   = "int64"
	TS      = "ts"
	Text    = "text"
	Bin     = "bin"
	Msg     = "msg"
	Bool    = "bool"
	Float64 = "float64"
	DS      = "ds"
	// TupleSet is a tuple definition followed by its tuples, all under the
	// field's key: data whose shape is only known when the message is built.
	TupleSet = "tupleset"
	Rec      = "record"
)

var baseTypes = map[string]bool{Int32: true, Int64: true, TS: true, Text: true, Bin: true, Msg: true,
	Bool: true, Float64: true, DS: true, TupleSet: true}

// Field is one declared item.
type Field struct {
	Name     string
	Type     string // one of the base types, or Rec
	Record   *Block // the record when Type is Rec
	Key      uint16
	List     bool   // repeated key, zero or more
	Optional bool   // may be absent
	Nullable bool   // may arrive as a null instead of a value
	File     string // the file it was read from
	Line     int
	typeName string // as written, resolved after the whole file is read
}

// Required reports whether a decoder must find the field.
func (f *Field) Required() bool { return !f.List && !f.Optional }

// Late reports whether the field is written after the single ones: a list
// or a tupleset, which take several items under their key.
func (f *Field) Late() bool { return f.List || f.Type == TupleSet }

// Block is a command, a reply or a record.
type Block struct {
	Kind   Kind
	Name   string
	Number uint32 // command number; a reply carries its command's
	Fields []*Field
	File   string // the file it was read from
	Line   int
}

// Names is a section "names [lang [side]]": the names one implementation,
// or all of them, give to what is generated.
type Names struct {
	Lang    string // go, c, harbour; "" for any
	Side    string // server, client; "" for any
	Package string // the Go package
	Prefix  string // what precedes a C or Harbour type with no name of its own
	Service string // the multiplexer (server) or the client object (client)
	Types   []*TypeName
	File    string
	Line    int
}

// TypeName names the type of one block, or of all those of a kind.
type TypeName struct {
	Kind  Kind
	Block string // the command or record; "*" for all
	Name  string // the name; with Block "*" it holds a * for each one's own
	File  string
	Line  int
}

// Languages a names section may be for.
const (
	Go      = "go"
	C       = "c"
	Harbour = "harbour"
)

// File is a whole declaration, blocks in the order written: those of the
// file given and of every file it includes.
type File struct {
	Name    string // as given to Parse, for messages
	Service string // the service it declares, "" for none
	Blocks  []*Block
	Names   []*Names
}

// sections lists the names sections that apply to lang and side, the most
// specific first. side "" asks for those with no side.
func (f *File) sections(lang, side string) []*Names {
	var out []*Names
	for _, wantLang := range []bool{true, false} {
		for _, wantSide := range []bool{true, false} {
			for _, s := range f.Names {
				if (s.Lang != "") != wantLang || (s.Side != "") != wantSide {
					continue
				}
				if (s.Lang == "" || s.Lang == lang) && (s.Side == "" || s.Side == side) {
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// TypeName returns the name the declaration gives the type of block b in
// lang, in lowercase with _, or "" when it names none. Within a section a
// line for the block itself wins over one for all.
func (f *File) TypeName(lang string, b *Block) string {
	own := b.Name // what * stands for: the command of a reply too
	for _, s := range f.sections(lang, "") {
		for _, exact := range []bool{true, false} {
			for _, t := range s.Types {
				if t.Kind != b.Kind || (t.Block == "*") == exact {
					continue
				}
				if exact && t.Block == own {
					return t.Name
				}
				if !exact {
					return strings.Replace(t.Name, "*", own, 1)
				}
			}
		}
	}
	return ""
}

// Package returns the Go package the declaration names, "" for none.
func (f *File) Package() string {
	for _, s := range f.sections(Go, "") {
		if s.Package != "" {
			return s.Package
		}
	}
	return ""
}

// Prefix returns the prefix the declaration names for lang, "" for none.
func (f *File) Prefix(lang string) string {
	for _, s := range f.sections(lang, "") {
		if s.Prefix != "" {
			return s.Prefix
		}
	}
	return ""
}

// ServiceName returns the name the declaration gives the service on one
// side of lang (the multiplexer or the client object), "" for none.
func (f *File) ServiceName(lang, side string) string {
	for _, s := range f.sections(lang, side) {
		if s.Service != "" {
			return s.Service
		}
	}
	return ""
}

// Commands, Replies and Records list the blocks of one kind, in order.
func (f *File) Commands() []*Block { return f.kind(Command) }
func (f *File) Replies() []*Block  { return f.kind(Reply) }
func (f *File) Records() []*Block  { return f.kind(Record) }

func (f *File) kind(k Kind) []*Block {
	var out []*Block
	for _, b := range f.Blocks {
		if b.Kind == k {
			out = append(out, b)
		}
	}
	return out
}

// Reserved field names: they would collide with the generated methods.
var reserved = map[string]bool{"pack": true, "unpack": true}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Error is a parse or check error at a line of the file.
type Error struct {
	File string
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg) }

// Parse reads a declaration from r. name is used in error messages. The
// text cannot include other files: there is nowhere to read them from. Use
// ParseFile for that.
func Parse(name string, r io.Reader) (*File, error) {
	f := &File{Name: name}
	p := &parser{f: f}
	if err := p.read(name, r, false); err != nil {
		return nil, err
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	return f, nil
}

// ParseFile reads the declaration in the file at path and in the files it
// includes.
func ParseFile(path string) (*File, error) {
	f := &File{Name: path}
	p := &parser{f: f, files: true, state: map[string]int{}}
	if err := p.file(path, false); err != nil {
		return nil, err
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	return f, nil
}

// parser reads one declaration out of one or more files.
type parser struct {
	f     *File
	files bool           // read from disk: include works
	state map[string]int // per file: 1 being read, 2 read
}

// file reads the file at path, once: a file included again is already in.
func (p *parser) file(path string, included bool) error {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = filepath.Clean(abs)
	}
	switch p.state[key] {
	case 1:
		return fmt.Errorf("%s: includes itself", path)
	case 2:
		return nil
	}
	p.state[key] = 1
	fh, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fh.Close()
	if err := p.read(path, fh, included); err != nil {
		return err
	}
	p.state[key] = 2
	return nil
}

// read adds the blocks of one text to the declaration. included says the
// text was reached through an include.
func (p *parser) read(name string, r io.Reader, included bool) error {
	f := p.f
	errf := func(line int, format string, a ...any) error {
		return &Error{File: name, Line: line, Msg: fmt.Sprintf(format, a...)}
	}
	var cur *Block
	var sec *Names // the names section being read, when in one
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		tok := strings.Fields(strings.ReplaceAll(line, "=", " = "))
		if len(tok) == 0 {
			continue
		}
		// a names section: its header has no "=", which a field called
		// names has
		if tok[0] == "names" && !strings.Contains(line, "=") {
			s, err := namesHeader(tok)
			if err != nil {
				return errf(n, "%s", err)
			}
			s.File, s.Line = name, n
			f.Names = append(f.Names, s)
			sec, cur = s, nil
			continue
		}
		if sec != nil {
			done, err := sec.entry(tok, name, n)
			if err != nil {
				return errf(n, "%s", err)
			}
			if done {
				continue
			}
		}
		// service and include stand outside the blocks; a field of that
		// name has its type and key after it
		if tok[0] == "service" && (cur == nil || len(tok) == 2) {
			if len(tok) != 2 {
				return errf(n, "expected %q", "service name")
			}
			if !nameRe.MatchString(tok[1]) {
				return errf(n, "service name %q: use lowercase letters, digits and _ (starting with a letter)", tok[1])
			}
			if included {
				return errf(n, "service %s: a file that declares a service cannot be included", tok[1])
			}
			if f.Service != "" {
				return errf(n, "service %s: the service is already declared (%s)", tok[1], f.Service)
			}
			f.Service = tok[1]
			continue
		}
		if tok[0] == "include" && (cur == nil || len(tok) == 2) {
			path := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "include"))
			if len(path) < 3 || path[0] != '"' || path[len(path)-1] != '"' {
				return errf(n, "expected %q", "include \"file.kvp\"")
			}
			if !p.files {
				return errf(n, "include: this text was not read from a file")
			}
			path = filepath.Join(filepath.Dir(name), filepath.FromSlash(path[1:len(path)-1]))
			if err := p.file(path, true); err != nil {
				if _, ok := err.(*Error); ok {
					return err
				}
				return errf(n, "include: %s", err)
			}
			continue
		}
		switch tok[0] {
		case "command", "record":
			if len(tok) != 4 || tok[2] != "=" {
				return errf(n, "expected %q", tok[0]+" name = number")
			}
			num, err := strconv.ParseUint(tok[3], 10, 32)
			if err != nil {
				return errf(n, "%s %s: number %q is not a uint32", tok[0], tok[1], tok[3])
			}
			kind := Command
			if tok[0] == "record" {
				kind = Record
			}
			cur = &Block{Kind: kind, Name: tok[1], Number: uint32(num), File: name, Line: n}
		case "reply":
			if len(tok) != 2 {
				return errf(n, "expected %q (a reply takes its command's number)", "reply name")
			}
			cur = &Block{Kind: Reply, Name: tok[1], File: name, Line: n}
		default:
			if cur == nil {
				return errf(n, "field %q outside a command, reply or record", tok[0])
			}
			fl, err := parseField(tok, n)
			if err != nil {
				return errf(n, "%s", err)
			}
			fl.File = name
			cur.Fields = append(cur.Fields, fl)
			continue
		}
		if !nameRe.MatchString(cur.Name) {
			return errf(n, "name %q: use lowercase letters, digits and _ (starting with a letter)", cur.Name)
		}
		f.Blocks = append(f.Blocks, cur)
		sec = nil // a block closes the names section
	}
	return sc.Err()
}

// namesHeader reads "names [lang [side]]".
func namesHeader(tok []string) (*Names, error) {
	const want = "names [go | c | harbour | *] [server | client | *]"
	if len(tok) > 3 {
		return nil, fmt.Errorf("expected %q", want)
	}
	s := &Names{}
	if len(tok) > 1 && tok[1] != "*" {
		if tok[1] != Go && tok[1] != C && tok[1] != Harbour {
			return nil, fmt.Errorf("names: unknown language %q (go, c, harbour or *)", tok[1])
		}
		s.Lang = tok[1]
	}
	if len(tok) > 2 && tok[2] != "*" {
		if tok[2] != "server" && tok[2] != "client" {
			return nil, fmt.Errorf("names: unknown side %q (server, client or *)", tok[2])
		}
		s.Side = tok[2]
	}
	return s, nil
}

// entry reads one line of a names section. done is false when the line is
// not one of its entries: a block header or a declaration, which close it.
func (s *Names) entry(tok []string, file string, line int) (done bool, err error) {
	switch tok[0] {
	case "package", "prefix", "service":
		if len(tok) == 2 && tok[0] == "service" {
			return false, nil // the declaration of the service
		}
		if len(tok) != 3 || tok[1] != "=" {
			return false, fmt.Errorf("names: expected %q", tok[0]+" = name")
		}
		if !nameRe.MatchString(tok[2]) {
			return false, fmt.Errorf("names: %s %q: use lowercase letters, digits and _ (starting with a letter)", tok[0], tok[2])
		}
		var slot *string
		switch tok[0] {
		case "package":
			if s.Lang != Go {
				return false, fmt.Errorf("names: package is of a go section")
			}
			slot = &s.Package
		case "prefix":
			if s.Lang == Go {
				return false, fmt.Errorf("names: prefix is of a c or harbour section (Go has package)")
			}
			slot = &s.Prefix
		default:
			slot = &s.Service
		}
		if *slot != "" {
			return false, fmt.Errorf("names: %s is already named (%s)", tok[0], *slot)
		}
		*slot = tok[2]
		return true, nil
	case "command", "reply", "record":
		if len(tok) != 4 || tok[2] != "=" {
			return false, nil // "reply name": a block
		}
		if _, err := strconv.ParseUint(tok[3], 10, 64); err == nil {
			return false, nil // "command name = number": a block
		}
		kind := map[string]Kind{"command": Command, "reply": Reply, "record": Record}[tok[0]]
		t := &TypeName{Kind: kind, Block: tok[1], Name: tok[3], File: file, Line: line}
		plain := t.Name
		if t.Block == "*" {
			if strings.Count(t.Name, "*") != 1 {
				return false, fmt.Errorf("names: %s * = %s: the name needs one * for each one's own name", tok[0], t.Name)
			}
			plain = strings.Replace(t.Name, "*", "x", 1)
		}
		if !nameRe.MatchString(plain) {
			return false, fmt.Errorf("names: %s %s = %q: use lowercase letters, digits and _ (starting with a letter)", tok[0], t.Block, t.Name)
		}
		if s.Side != "" {
			return false, fmt.Errorf("names: a type is named per language, in a section with no side (both sides share the types)")
		}
		for _, o := range s.Types {
			if o.Kind == t.Kind && o.Block == t.Block {
				return false, fmt.Errorf("names: %s %s is already named (%s)", tok[0], t.Block, o.Name)
			}
		}
		s.Types = append(s.Types, t)
		return true, nil
	}
	return false, fmt.Errorf("names: unknown entry %q (package, prefix, service, command, reply or record)", tok[0])
}

func parseField(tok []string, line int) (*Field, error) {
	if len(tok) < 4 || tok[2] != "=" {
		return nil, fmt.Errorf("expected %q", "name type = key [list | optional] [nullable]")
	}
	fl := &Field{Name: tok[0], typeName: tok[1], Line: line}
	if !nameRe.MatchString(fl.Name) {
		return nil, fmt.Errorf("field name %q: use lowercase letters, digits and _ (starting with a letter)", fl.Name)
	}
	if reserved[fl.Name] {
		return nil, fmt.Errorf("field name %q is reserved", fl.Name)
	}
	key, err := strconv.ParseUint(tok[3], 10, 16)
	if err != nil || key == 0 {
		return nil, fmt.Errorf("field %s: key %q must be 1..65535 (0 is the error text)", fl.Name, tok[3])
	}
	fl.Key = uint16(key)
	for _, mod := range tok[4:] {
		switch mod {
		case "list":
			fl.List = true
		case "optional":
			fl.Optional = true
		case "nullable":
			fl.Nullable = true
		default:
			return nil, fmt.Errorf("field %s: unknown modifier %q (list, optional or nullable)", fl.Name, mod)
		}
	}
	if fl.List && fl.Optional {
		return nil, fmt.Errorf("field %s: a list is already optional", fl.Name)
	}
	if fl.List && fl.Nullable {
		return nil, fmt.Errorf("field %s: a list cannot be nullable", fl.Name)
	}
	if fl.typeName == TupleSet && fl.List {
		return nil, fmt.Errorf("field %s: a tupleset is already a list", fl.Name)
	}
	if fl.typeName == TupleSet && fl.Nullable {
		return nil, fmt.Errorf("field %s: a tupleset cannot be nullable", fl.Name)
	}
	return fl, nil
}

// check resolves record types and enforces the uniqueness rules.
func (f *File) check() error {
	file := f.Name // of the block being checked
	errf := func(line int, format string, a ...any) error {
		return &Error{File: file, Line: line, Msg: fmt.Sprintf(format, a...)}
	}
	// at names where another declaration is, the file too when it is another
	at := func(o *Block) string {
		if o.File != file {
			return fmt.Sprintf("%s:%d", o.File, o.Line)
		}
		return fmt.Sprintf("line %d", o.Line)
	}
	if f.Service != "" && len(f.Commands()) == 0 {
		return &Error{File: f.Name, Line: 1, Msg: fmt.Sprintf("service %s: a service has one or more commands", f.Service)}
	}
	names := map[string]*Block{}   // commands and records share names
	numbers := map[uint32]*Block{} // and numbers
	for _, b := range f.Blocks {
		file = b.File
		if b.Kind == Reply {
			continue
		}
		if o := names[b.Name]; o != nil {
			return errf(b.Line, "%s %s: name already used at %s", b.Kind, b.Name, at(o))
		}
		if o := numbers[b.Number]; o != nil {
			return errf(b.Line, "%s %s: number %d already used by %s %s", b.Kind, b.Name, b.Number, o.Kind, o.Name)
		}
		if baseTypes[b.Name] {
			return errf(b.Line, "%s %s: the name is a type", b.Kind, b.Name)
		}
		names[b.Name] = b
		numbers[b.Number] = b
	}
	replied := map[string]*Block{}
	for _, b := range f.Blocks {
		file = b.File
		if b.Kind != Reply {
			continue
		}
		cmd := names[b.Name]
		if cmd == nil || cmd.Kind != Command {
			return errf(b.Line, "reply %s: no command %s", b.Name, b.Name)
		}
		if o := replied[b.Name]; o != nil {
			return errf(b.Line, "reply %s: already declared at %s", b.Name, at(o))
		}
		if o := names[b.Name+"_reply"]; o != nil {
			file = o.File
			return errf(o.Line, "%s %s: the name collides with the reply of command %s", o.Kind, o.Name, b.Name)
		}
		replied[b.Name] = b
		b.Number = cmd.Number
	}
	for _, s := range f.Names {
		for _, t := range s.Types {
			if t.Block == "*" {
				continue
			}
			file = t.File
			var ok bool
			switch t.Kind {
			case Reply:
				ok = replied[t.Block] != nil
			default:
				ok = names[t.Block] != nil && names[t.Block].Kind == t.Kind
			}
			if !ok {
				return errf(t.Line, "names: no %s %s", t.Kind, t.Block)
			}
		}
	}
	for _, b := range f.Blocks {
		file = b.File
		keys := map[uint16]*Field{}
		fields := map[string]*Field{}
		for _, fl := range b.Fields {
			if o := keys[fl.Key]; o != nil {
				return errf(fl.Line, "%s %s: key %d already used by %s", b.Kind, b.Name, fl.Key, o.Name)
			}
			if o := fields[fl.Name]; o != nil {
				return errf(fl.Line, "%s %s: field %s already declared at line %d", b.Kind, b.Name, fl.Name, o.Line)
			}
			keys[fl.Key] = fl
			fields[fl.Name] = fl
			if baseTypes[fl.typeName] {
				fl.Type = fl.typeName
				continue
			}
			r := names[fl.typeName]
			if r == nil || r.Kind != Record {
				return errf(fl.Line, "field %s: unknown type %q (%s or a record)", fl.Name, fl.typeName, typeList())
			}
			fl.Type = Rec
			fl.Record = r
		}
		for _, fl := range b.Fields {
			if o := fields[fl.Name+"_null"]; o != nil && fl.Nullable {
				return errf(o.Line, "%s %s: field %s collides with the null flag of %s", b.Kind, b.Name, o.Name, fl.Name)
			}
		}
	}
	return nil

}

func typeList() string {
	var t []string
	for k := range baseTypes {
		t = append(t, k)
	}
	sort.Strings(t)
	return strings.Join(t, ", ")
}

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
// "name type = key [list | optional]". A reply takes the number of its
// command. Fields are required unless marked optional; a list may be empty.
package schema

import (
	"bufio"
	"fmt"
	"io"
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
	Rec     = "record"
)

var baseTypes = map[string]bool{Int32: true, Int64: true, TS: true, Text: true, Bin: true, Msg: true,
	Bool: true, Float64: true, DS: true}

// Field is one declared item.
type Field struct {
	Name     string
	Type     string // one of the base types, or Rec
	Record   *Block // the record when Type is Rec
	Key      uint16
	List     bool // repeated key, zero or more
	Optional bool // may be absent
	Line     int
	typeName string // as written, resolved after the whole file is read
}

// Required reports whether a decoder must find the field.
func (f *Field) Required() bool { return !f.List && !f.Optional }

// Block is a command, a reply or a record.
type Block struct {
	Kind   Kind
	Name   string
	Number uint32 // command number; a reply carries its command's
	Fields []*Field
	Line   int
}

// File is a whole declaration, blocks in the order written.
type File struct {
	Name   string // as given to Parse, for messages
	Blocks []*Block
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

// Parse reads a declaration from r. name is used in error messages.
func Parse(name string, r io.Reader) (*File, error) {
	f := &File{Name: name}
	errf := func(line int, format string, a ...any) error {
		return &Error{File: name, Line: line, Msg: fmt.Sprintf(format, a...)}
	}
	var cur *Block
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
		switch tok[0] {
		case "command", "record":
			if len(tok) != 4 || tok[2] != "=" {
				return nil, errf(n, "expected %q", tok[0]+" name = number")
			}
			num, err := strconv.ParseUint(tok[3], 10, 32)
			if err != nil {
				return nil, errf(n, "%s %s: number %q is not a uint32", tok[0], tok[1], tok[3])
			}
			kind := Command
			if tok[0] == "record" {
				kind = Record
			}
			cur = &Block{Kind: kind, Name: tok[1], Number: uint32(num), Line: n}
		case "reply":
			if len(tok) != 2 {
				return nil, errf(n, "expected %q (a reply takes its command's number)", "reply name")
			}
			cur = &Block{Kind: Reply, Name: tok[1], Line: n}
		default:
			if cur == nil {
				return nil, errf(n, "field %q outside a command, reply or record", tok[0])
			}
			fl, err := parseField(tok, n)
			if err != nil {
				return nil, errf(n, "%s", err)
			}
			cur.Fields = append(cur.Fields, fl)
			continue
		}
		if !nameRe.MatchString(cur.Name) {
			return nil, errf(n, "name %q: use lowercase letters, digits and _ (starting with a letter)", cur.Name)
		}
		f.Blocks = append(f.Blocks, cur)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	return f, nil
}

func parseField(tok []string, line int) (*Field, error) {
	if len(tok) < 4 || tok[2] != "=" {
		return nil, fmt.Errorf("expected %q", "name type = key [list | optional]")
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
		default:
			return nil, fmt.Errorf("field %s: unknown modifier %q (list or optional)", fl.Name, mod)
		}
	}
	if fl.List && fl.Optional {
		return nil, fmt.Errorf("field %s: a list is already optional", fl.Name)
	}
	return fl, nil
}

// check resolves record types and enforces the uniqueness rules.
func (f *File) check() error {
	errf := func(line int, format string, a ...any) error {
		return &Error{File: f.Name, Line: line, Msg: fmt.Sprintf(format, a...)}
	}
	names := map[string]*Block{}   // commands and records share names
	numbers := map[uint32]*Block{} // and numbers
	for _, b := range f.Blocks {
		if b.Kind == Reply {
			continue
		}
		if o := names[b.Name]; o != nil {
			return errf(b.Line, "%s %s: name already used at line %d", b.Kind, b.Name, o.Line)
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
		if b.Kind != Reply {
			continue
		}
		cmd := names[b.Name]
		if cmd == nil || cmd.Kind != Command {
			return errf(b.Line, "reply %s: no command %s", b.Name, b.Name)
		}
		if o := replied[b.Name]; o != nil {
			return errf(b.Line, "reply %s: already declared at line %d", b.Name, o.Line)
		}
		if o := names[b.Name+"_reply"]; o != nil {
			return errf(o.Line, "%s %s: the name collides with the reply of command %s", o.Kind, o.Name, b.Name)
		}
		replied[b.Name] = b
		b.Number = cmd.Number
	}
	for _, b := range f.Blocks {
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

package gen

import (
	"fmt"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// hbTypes maps a base type to its KVP_* constant, the value a variable of
// that type starts with (and an absent optional field reads as), and the
// Add function. What starts as NIL has no INIT.
var hbTypes = map[string]struct{ kvp, zero, add string }{
	schema.Int32:    {"KVP_INT32", "0", "KvpAddInt32"},
	schema.Int64:    {"KVP_INT64", "0", "KvpAddInt64"},
	schema.TS:       {"KVP_TS", "0", "KvpAddTs"},
	schema.Text:     {"KVP_TEXT", `""`, "KvpAddText"},
	schema.Bin:      {"KVP_BIN", `""`, "KvpAddBin"},
	schema.Msg:      {"KVP_MSG", "NIL", "KvpAddMsg"},
	schema.Bool:     {"KVP_BOOL", ".F.", "KvpAddBool"},
	schema.Float64:  {"KVP_FLOAT64", "0", "KvpAddFloat64"},
	schema.DS:       {"KVP_DS", `hb_SToD( "" )`, "KvpAddDs"},
	schema.Rec:      {"KVP_MSG", "NIL", "KvpAddMsg"},
	schema.TupleSet: {"KVP_TUPLEDEF", "NIL", ""},
}

// hbReserved are the field names a Harbour class cannot take: they are
// messages every object already answers, or methods of the generated class.
var hbReserved = map[string]bool{
	"new": true, "init": true, "encode": true, "decode": true, "error": true, "self": true, "super": true,
	"classname": true, "classh": true, "classsel": true, "isderivedfrom": true, "iskindof": true, "msgnotfound": true,
}

// Harbour generates a .ch with the numbers and keys and a .prg with a class
// per block, over the classes of kvpclass.prg: <P><Block> inherits
// KvpStruct (Encode, Decode) and has a variable per field and the methods
// Pack() --> writer handle and Unpack( hMsg, @nErr ) --> .T. or .F. A list
// is an array, a record an object of its own class, a tupleset a
// KvpTupleSet, and a nullable field that is null holds NIL. chname is the
// .ch file name the .prg includes.
func Harbour(f *schema.File, prefix, chname string) (ch, prg []byte, err error) {
	if !prefixRe.MatchString(prefix) {
		return nil, nil, fmt.Errorf("Harbour prefix %q: use lowercase letters, digits and _", prefix)
	}
	if err := cCheck(f, prefix); err != nil { // same names, same collisions
		return nil, nil, err
	}
	if err := holdsItself(f); err != nil {
		return nil, nil, err
	}
	for _, bl := range f.Blocks {
		for _, fl := range bl.Fields {
			if hbReserved[fl.Name] {
				return nil, nil, fmt.Errorf("%s:%d: field name %q is taken in a Harbour class", f.Name, fl.Line, fl.Name)
			}
		}
	}
	P := strings.ToUpper(prefix)
	var cb, pb strings.Builder
	cw := func(format string, a ...any) { fmt.Fprintf(&cb, format, a...) }
	pw := func(format string, a ...any) { fmt.Fprintf(&pb, format, a...) }

	guard := P + "_KVP_CH"
	cw("/* %s */\n#ifndef %s\n#define %s\n\n", header(f), guard, guard)
	for _, bl := range f.Blocks {
		if bl.Kind != schema.Reply {
			cw("#define %s %d\n", cNum(P, bl), bl.Number)
		}
	}
	for _, bl := range f.Blocks {
		if len(bl.Fields) == 0 {
			continue
		}
		cw("\n/* Keys of %s %s. */\n", bl.Kind, bl.Name)
		for _, fl := range bl.Fields {
			cw("#define %s %d\n", cKey(P, bl, fl), fl.Key)
		}
	}
	cw("\n#endif\n")

	pw("/* %s */\n%s", header(f), hbUsage)
	pw("#include \"hbclass.ch\"\n#include \"common.ch\"\n#include \"kvpack.ch\"\n#include \"%s\"\n", baseName(chname))
	for _, bl := range f.Blocks {
		hbBlock(pw, prefix, P, bl)
	}
	return []byte(cb.String()), []byte(pb.String()), nil
}

const hbUsage = `
/*
 * Per block a class over KvpStruct (kvpclass.prg), with a variable per
 * field:
 *   o := X():New()            every variable at the empty value of its
 *                             type: a zero, "", an empty date, {}, a
 *                             record of empty fields, a message of
 *                             command 0 and no items, a KvpTupleSet of no
 *                             fields. Only an optional record or message
 *                             starts NIL: there is none
 *   o:Encode()                the message bytes
 *   o:Decode( cBytes, @nErr ) .T., or .F. with nErr set: KVP_E_MISSING when
 *                             a required field is absent, KVP_E_REPLY for
 *                             an error reply (read it with KvpErrorText)
 *   o:Pack(), o:Unpack( hMsg, @nErr )   the same over a writer handle and a
 *                             parsed message
 * A list is an array; a record is an object of its own class (so is each
 * element of a list of records); a tupleset is a KvpTupleSet.
 *
 * What is written follows the declaration: a field always; an optional one
 * when it is not empty; a nullable one always, as a null when it holds NIL;
 * an optional nullable one when it is not NIL. Reading mirrors it: an
 * optional field that did not arrive reads empty, an optional nullable one
 * reads NIL. So a new object writes every field that is not optional, as
 * its empty value; NIL is a null only where the field is nullable, and
 * anywhere else it is written as the empty value.
 */

`

func hbName(prefix, block string) string { return camel(prefix) + camel(block) }

// hbPresent is the test that an optional field of that type is not empty.
func hbPresent(fl *schema.Field, v string) string {
	switch fl.Type {
	case schema.Text, schema.Bin:
		return fmt.Sprintf("HB_ISSTRING( %s ) .AND. Len( %s ) > 0", v, v) // spaces are not empty
	case schema.Msg, schema.Rec:
		return v + " != NIL"
	}
	return "! Empty( " + v + " )"
}

func hbBlock(pw func(string, ...any), prefix, P string, bl *schema.Block) {
	cls := hbName(prefix, blockName(bl))
	hasList, hasRec, hasSet := false, false, false
	width := 0
	for _, fl := range bl.Fields {
		if fl.List {
			hasList = true
		}
		if fl.Type == schema.Rec {
			hasRec = true
		}
		if fl.Type == schema.TupleSet {
			hasSet = true
		}
		width = max(width, len(fl.Name))
	}

	// class
	switch bl.Kind {
	case schema.Command:
		pw("\n/* %s: the request of command %s (%d). */\n", cls, bl.Name, bl.Number)
	case schema.Reply:
		pw("\n/* %s: the reply to command %s (%d). */\n", cls, bl.Name, bl.Number)
	default:
		pw("\n/* %s: record %s (%d). */\n", cls, bl.Name, bl.Number)
	}
	pw("CREATE CLASS %s INHERIT KvpStruct\n\n", cls)
	for _, fl := range bl.Fields {
		init, what := hbTypes[fl.Type].zero, fl.Type
		switch {
		case fl.List && fl.Type == schema.Rec:
			init, what = "{}", "list of "+hbName(prefix, fl.Record.Name)
		case fl.List:
			init, what = "{}", "list of "+fl.Type
		case fl.Type == schema.Rec:
			what = hbName(prefix, fl.Record.Name)
		case fl.Type == schema.TupleSet:
			what = "KvpTupleSet"
		}
		if fl.Optional {
			what += ", optional"
		}
		if fl.Nullable {
			what += ", nullable"
		}
		decl := fmt.Sprintf("   VAR %-*s", width, fl.Name)
		if init != "NIL" {
			decl += " INIT " + init
		}
		pw("%-*s   // key %d: %s\n", width+26, decl, fl.Key, what)
	}
	if len(bl.Fields) > 0 {
		pw("\n")
	}
	// what cannot be an INIT value: every object needs its own
	var made []string
	for _, fl := range bl.Fields {
		switch {
		case fl.List:
		case fl.Type == schema.TupleSet:
			made = append(made, fmt.Sprintf("   ::%s := KvpTupleSet():New()\n", fl.Name))
		case fl.Optional && !fl.Nullable:
		case fl.Type == schema.Rec:
			made = append(made, fmt.Sprintf("   ::%s := %s():New()\n", fl.Name, hbName(prefix, fl.Record.Name)))
		case fl.Type == schema.Msg:
			made = append(made, fmt.Sprintf("   ::%s := KvpNew( 0 )\n", fl.Name))
		}
	}
	if len(made) > 0 {
		pw("   METHOD Init()\n")
	}
	pw("   METHOD Pack()\n   METHOD Unpack( hMsg, nErr )\n\nENDCLASS\n")
	if len(made) > 0 {
		pw("\n/* Init gives a new object what always travels and is an object itself. */\n")
		pw("METHOD Init() CLASS %s\n\n%s\n   RETURN Self\n", cls, strings.Join(made, ""))
	}

	// Pack
	pw("\nMETHOD Pack() CLASS %s\n\n   LOCAL hMsg := KvpNew( %s )\n", cls, cNum(P, bl))
	if hasList {
		pw("   LOCAL x\n")
	}
	pw("\n")
	for _, fl := range packOrder(bl) {
		key := cKey(P, bl, fl)
		t := hbTypes[fl.Type]
		v := "::" + fl.Name
		val := func(x string) string {
			if fl.Type == schema.Rec {
				return x + ":Pack()"
			}
			return x
		}
		add := fmt.Sprintf("%s( hMsg, %s, %s )", t.add, key, val(v))
		// a record or a message that always travels and holds NIL where NIL
		// is no null: the empty one
		addAlways := add
		switch fl.Type {
		case schema.Rec:
			addAlways = fmt.Sprintf("%s( hMsg, %s, iif( %s == NIL, %s():New(), %s ):Pack() )", t.add, key, v, hbName(prefix, fl.Record.Name), v)
		case schema.Msg:
			addAlways = fmt.Sprintf("%s( hMsg, %s, iif( %s == NIL, KvpNew( 0 ), %s ) )", t.add, key, v, v)
		}
		switch {
		case fl.Type == schema.TupleSet && fl.Optional: // no fields is the empty one
			pw("   IF %s != NIL .AND. Len( %s:aFields ) > 0\n      %s:AddTo( hMsg, %s )\n   ENDIF\n", v, v, v, key)
		case fl.Type == schema.TupleSet:
			pw("   iif( %s == NIL, KvpTupleSet():New(), %s ):AddTo( hMsg, %s )\n", v, v, key)
		case fl.List:
			pw("   FOR EACH x IN %s\n      %s( hMsg, %s, %s )\n   NEXT\n", v, t.add, key, val("x"))
		case fl.Nullable && fl.Optional: // written when it is not null, whatever its value
			pw("   IF %s != NIL\n      %s\n   ENDIF\n", v, add)
		case fl.Nullable:
			pw("   IF %s == NIL\n      KvpAddNull( hMsg, %s )\n   ELSE\n      %s\n   ENDIF\n", v, key, add)
		case fl.Optional:
			pw("   IF %s\n      %s\n   ENDIF\n", hbPresent(fl, v), add)
		default:
			pw("   %s\n", addAlways)
		}
	}
	pw("\n   RETURN hMsg\n")

	// Unpack
	pw("\nMETHOD Unpack( hMsg, nErr ) CLASS %s\n\n", cls)
	if hasList {
		pw("   LOCAL aItem, nI\n")
	}
	if hasRec {
		pw("   LOCAL oSub\n")
	}
	if hasSet {
		pw("   LOCAL oSet\n")
	}
	if hasList || hasRec || hasSet {
		pw("\n")
	}
	pw("   nErr := KVP_OK\n")
	if len(bl.Fields) == 0 && bl.Kind != schema.Reply {
		pw("   HB_SYMBOL_UNUSED( hMsg )\n")
	}
	if bl.Kind == schema.Reply {
		pw("   IF KvpErrorText( hMsg ) != NIL\n      nErr := KVP_E_REPLY\n      RETURN .F.\n   ENDIF\n")
	}
	for _, fl := range bl.Fields {
		key := cKey(P, bl, fl)
		t := hbTypes[fl.Type]
		v := "::" + fl.Name
		// what a field that did not arrive, or arrived as another type, does
		absent := fmt.Sprintf("      %s := %s\n", v, t.zero)
		switch {
		case fl.Required():
			absent = "      nErr := KVP_E_MISSING\n      RETURN .F.\n"
		case fl.Nullable: // optional nullable: absent is null
			absent = fmt.Sprintf("      %s := NIL\n", v)
		}
		switch {
		case fl.Type == schema.TupleSet && fl.Optional: // absent reads empty: no fields
			pw("   oSet := KvpTupleSet():New()\n   oSet:Read( hMsg, %s )\n   %s := oSet\n", key, v)
		case fl.Type == schema.TupleSet:
			pw("   oSet := KvpTupleSet():New()\n   IF oSet:Read( hMsg, %s )\n      %s := oSet\n   ELSE\n%s   ENDIF\n", key, v, absent)
		case fl.List:
			pw("   %s := {}\n", v)
			pw("   FOR nI := 1 TO KvpCount( hMsg )\n      aItem := KvpItem( hMsg, nI )\n")
			pw("      IF aItem[ KVP_ITEM_KEY ] == %s .AND. aItem[ KVP_ITEM_TYPE ] == %s\n", key, t.kvp)
			if fl.Type == schema.Rec {
				pw("         oSub := %s():New()\n", hbName(prefix, fl.Record.Name))
				pw("         IF ! oSub:Unpack( aItem[ KVP_ITEM_VALUE ], @nErr )\n            RETURN .F.\n         ENDIF\n")
				pw("         AAdd( %s, oSub )\n", v)
			} else {
				pw("         AAdd( %s, aItem[ KVP_ITEM_VALUE ] )\n", v)
			}
			pw("      ENDIF\n   NEXT\n")
		default:
			first := "IF"
			if fl.Nullable && !fl.Optional { // a null arrives as a null
				pw("   IF KvpIsNull( hMsg, %s )\n      %s := NIL\n", key, v)
				first = "ELSEIF"
			}
			pw("   %s KvpType( hMsg, %s ) == %s\n", first, key, t.kvp)
			if fl.Type == schema.Rec {
				pw("      oSub := %s():New()\n", hbName(prefix, fl.Record.Name))
				pw("      IF ! oSub:Unpack( KvpGet( hMsg, %s ), @nErr )\n         RETURN .F.\n      ENDIF\n", key)
				pw("      %s := oSub\n", v)
			} else {
				pw("      %s := KvpGet( hMsg, %s )\n", v, key)
			}
			pw("   ELSE\n%s   ENDIF\n", absent)
		}
	}
	pw("\n   RETURN .T.\n")
}

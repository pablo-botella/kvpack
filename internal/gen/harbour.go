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
	tn, err := typeNames(f, schema.Harbour, prefix)
	if err != nil {
		return nil, nil, err
	}
	if err := cCheck(f, prefix, tn); err != nil { // same names, same collisions
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

	sv, err := hbService(f, prefix, tn)
	if err != nil {
		return nil, nil, err
	}

	pw("/* %s */\n%s", header(f), hbUsage)
	if sv != nil {
		pw("%s", hbServerUsage)
	}
	pw("#include \"hbclass.ch\"\n#include \"common.ch\"\n#include \"kvpack.ch\"\n#include \"%s\"\n", baseName(chname))
	for _, bl := range f.Blocks {
		hbBlock(pw, tn, P, bl)
	}
	if sv != nil {
		hbServer(pw, sv, tn, P)
		hbClient(pw, sv, tn)
	}
	return []byte(cb.String()), []byte(pb.String()), nil
}

// hbServerReserved are the command names the multiplexer class cannot take
// as a variable, beyond those no class can.
var hbServerReserved = map[string]bool{"handler": true, "dispatch": true}

// hbSvc is what the server side of a declaration is called in Harbour.
type hbSvc struct {
	f      *schema.File
	client string            // the client class
	class  string            // the multiplexer
	msg    map[string]string // per command, the class of its message
	reply  map[string]*schema.Block
	prefix string
}

// hbService names the server side: the multiplexer and, per command, the
// class of the message of a call. nil when the declaration has no command.
func hbService(f *schema.File, prefix string, tn names) (*hbSvc, error) {
	if len(f.Commands()) == 0 {
		return nil, nil
	}
	name := f.ServiceName(schema.Harbour, "server")
	if name == "" {
		name = prefix + "_server"
	}
	sv := &hbSvc{f: f, class: camel(name), msg: map[string]string{}, reply: map[string]*schema.Block{}, prefix: prefix}
	cname := f.ServiceName(schema.Harbour, "client")
	if cname == "" {
		cname = prefix + "_client"
	}
	sv.client = camel(cname)
	if strings.EqualFold(sv.client, sv.class) {
		return nil, fmt.Errorf("%s: the client and the multiplexer are both called %s in Harbour", f.Name, sv.client)
	}
	taken := map[string]string{strings.ToUpper(sv.class): "the multiplexer", strings.ToUpper(sv.client): "the client"}
	for b, n := range tn {
		taken[strings.ToUpper(camel(n))] = b.Kind.String() + " " + b.Name
	}
	for _, b := range f.Replies() {
		sv.reply[b.Name] = b
	}
	for _, b := range f.Commands() {
		if hbReserved[b.Name] || hbServerReserved[b.Name] {
			return nil, fmt.Errorf("%s:%d: command name %q is taken in the Harbour multiplexer class", b.File, b.Line, b.Name)
		}
		if hbClientReserved[b.Name] {
			return nil, fmt.Errorf("%s:%d: command name %q is taken in the Harbour client class", b.File, b.Line, b.Name)
		}
		cls := camel(prefix+"_"+b.Name) + "Msg"
		if o, ok := taken[strings.ToUpper(cls)]; ok { // Harbour does not tell case apart
			return nil, fmt.Errorf("%s:%d: the message class %s of command %s has the name of %s", b.File, b.Line, cls, b.Name, o)
		}
		taken[strings.ToUpper(cls)] = "the message of command " + b.Name
		sv.msg[b.Name] = cls
	}
	return sv, nil
}

const hbServerUsage = `/*
 * The server side, with no transport in it. Three pieces:
 *   the handler   an object of the application with a method On<Command> per
 *                 command: OnLogin( oMsg, oCtx ). It is given to the
 *                 multiplexer
 *   the message   oMsg, created by the multiplexer for each call from the
 *                 class of its command:
 *                   oMsg:request   the request, unpacked
 *                   oMsg:reply     the reply, new and empty, to fill (NIL
 *                                  when the command declares no reply)
 *                   oMsg:Pack()    the reply as a message; oMsg:Encode() its
 *                                  bytes; oMsg:PackError( cText ) an error
 *                                  reply instead
 *   the context   oCtx, whatever the caller of Dispatch gives for the call:
 *                 the multiplexer passes it on and never looks at it
 *
 *   oSrv := <Service>():New( oHandler )
 *   lOk  := oSrv:Dispatch( hMsg, oCtx, @nErr )   hMsg a parsed message
 *
 * Dispatch creates the message of the command, unpacks the request into it
 * and calls the handler; that is all it does. What is replied, and how it
 * reaches the caller, is the handler's, with oMsg and oCtx. It says .F. and
 * calls nobody when the command is none of the service (KVP_E_NOT_FOUND) or
 * the request misses a required field (KVP_E_MISSING).
 *
 * The client side, with no transport in it either:
 *   oCli := <Client>():New( oTransport )
 *   oReply := oCli:Login( oReq, @nErr )   an object of the reply's class, or
 *                                         NIL with nErr set; a command that
 *                                         declares no reply gives .T. or .F.
 * oTransport is any object with a method Call( cRequest, @nErr ) that sends
 * the bytes of a request and returns those of the reply, or NIL when it
 * could not (nErr is then its own, or KVP_E_CALL). nErr is KVP_E_REPLY when
 * the server replied with an error, whose text is then in oCli:cError, and
 * KVP_E_MISSING when the reply misses a required field.
 */

`

// hbServer writes the message class of every command and the multiplexer.
func hbServer(pw func(string, ...any), sv *hbSvc, tn names, P string) {
	cmds := sv.f.Commands()
	for _, b := range cmds {
		cls := sv.msg[b.Name]
		pw("\n/* %s: the message of a call to command %s (%d). */\n", cls, b.Name, b.Number)
		pw("CREATE CLASS %s INHERIT KvpMsg\n\n   METHOD New( hMsg, nErr )\n\nENDCLASS\n", cls)
		pw("\nMETHOD New( hMsg, nErr ) CLASS %s\n\n", cls)
		pw("   ::nCommand := %s\n", cNum(P, b))
		pw("   ::request := %s():New()\n", tn.camel(b))
		if r := sv.reply[b.Name]; r != nil {
			pw("   ::reply := %s():New()\n", tn.camel(r))
		}
		pw("   ::request:Unpack( hMsg, @nErr )\n\n   RETURN Self\n")
	}

	width := len("handler")
	for _, b := range cmds {
		width = max(width, len(b.Name))
	}
	what := "the commands of " + baseName(sv.f.Name)
	if sv.f.Service != "" {
		what = "service " + sv.f.Service
	}
	pw("\n/* %s: the multiplexer of %s. */\n", sv.class, what)
	pw("CREATE CLASS %s\n\n", sv.class)
	pw("   VAR %-*s   // the application's object: a method On<Command>( oMsg, oCtx ) per command\n", width, "handler")
	for _, b := range cmds {
		pw("   VAR %-*s   // the class function of the message of command %s\n", width, b.Name, b.Name)
	}
	pw("\n   METHOD Init( oHandler )\n   METHOD Dispatch( hMsg, oCtx, nErr )\n\nENDCLASS\n")
	pw("\nMETHOD Init( oHandler ) CLASS %s\n\n   ::handler := oHandler\n", sv.class)
	for _, b := range cmds {
		pw("   ::%s := @%s()\n", b.Name, sv.msg[b.Name])
	}
	pw("\n   RETURN Self\n")
	pw("\nMETHOD Dispatch( hMsg, oCtx, nErr ) CLASS %s\n\n   LOCAL oMsg\n\n   nErr := KVP_OK\n   SWITCH KvpCommand( hMsg )\n", sv.class)
	for _, b := range cmds {
		pw("   CASE %s\n", cNum(P, b))
		pw("      oMsg := ::%s:exec():New( hMsg, @nErr )\n", b.Name)
		pw("      IF nErr == KVP_OK\n         ::handler:On%s( oMsg, oCtx )\n      ENDIF\n      EXIT\n", camel(b.Name))
	}
	pw("   OTHERWISE\n      nErr := KVP_E_NOT_FOUND\n   ENDSWITCH\n\n   RETURN nErr == KVP_OK\n")
}

// HarbourHandler generates the skeleton of the handler an application
// writes for the server side: a class with a method per command, to copy
// and fill. It is no part of what is compiled. chname is the .ch file name
// of the generated code.
func HarbourHandler(f *schema.File, prefix, chname string) ([]byte, error) {
	tn, err := typeNames(f, schema.Harbour, prefix)
	if err != nil {
		return nil, err
	}
	sv, err := hbService(f, prefix, tn)
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
	w("// Skeleton of the handler of %s, generated by kvpack: the object an\n", baseName(f.Name))
	w("// application gives to the multiplexer, with a method per command.\n")
	w("// Copy it under a name of your own and fill it; this file is written\n")
	w("// again every time the code is generated.\n")
	w("//\n//    oSrv := %s():New( MyHandler():New() )\n//    oSrv:Dispatch( hMsg, oCtx, @nErr )\n\n", sv.class)
	w("#include \"hbclass.ch\"\n#include \"common.ch\"\n#include \"kvpack.ch\"\n#include \"%s\"\n\n", baseName(chname))
	w("CREATE CLASS MyHandler\n\n")
	for _, c := range f.Commands() {
		w("   METHOD On%s( oMsg, oCtx )\n", camel(c.Name))
	}
	w("\nENDCLASS\n")
	for _, c := range f.Commands() {
		w("\n// Command %s (%d).\n", c.Name, c.Number)
		w("//    oMsg:request   %s: %s\n", tn.camel(c), fields(c))
		if r := sv.reply[c.Name]; r != nil {
			w("//    oMsg:reply     %s, new and empty, to fill: %s\n", tn.camel(r), fields(r))
		} else {
			w("//    oMsg:reply     NIL: the command declares no reply\n")
		}
		w("//    oMsg:Pack()    the reply as a message\n")
		w("//    oCtx           what the caller of Dispatch gave for this call\n")
		w("METHOD On%s( oMsg, oCtx ) CLASS MyHandler\n\n", camel(c.Name))
		w("   HB_SYMBOL_UNUSED( oMsg )\n   HB_SYMBOL_UNUSED( oCtx )\n\n   RETURN NIL\n")
	}
	return []byte(b.String()), nil
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

func hbBlock(pw func(string, ...any), tn names, P string, bl *schema.Block) {
	cls := tn.camel(bl)
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
			init, what = "{}", "list of "+tn.camel(fl.Record)
		case fl.List:
			init, what = "{}", "list of "+fl.Type
		case fl.Type == schema.Rec:
			what = tn.camel(fl.Record)
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
			made = append(made, fmt.Sprintf("   ::%s := %s():New()\n", fl.Name, tn.camel(fl.Record)))
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
			addAlways = fmt.Sprintf("%s( hMsg, %s, iif( %s == NIL, %s():New(), %s ):Pack() )", t.add, key, v, tn.camel(fl.Record), v)
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
				pw("         oSub := %s():New()\n", tn.camel(fl.Record))
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
				pw("      oSub := %s():New()\n", tn.camel(fl.Record))
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

// hbClientReserved are the command names the client class cannot take as a
// method, beyond those no class can.
var hbClientReserved = map[string]bool{"transport": true, "cerror": true}

// hbClient writes the client class: a method per command over a transport.
func hbClient(pw func(string, ...any), sv *hbSvc, tn names) {
	cmds := sv.f.Commands()
	what := "the commands of " + baseName(sv.f.Name)
	if sv.f.Service != "" {
		what = "service " + sv.f.Service
	}
	pw("\n/* %s: the calls to %s, over a transport. */\n", sv.client, what)
	pw("CREATE CLASS %s\n\n", sv.client)
	pw("   VAR transport       // an object with Call( cRequest, @nErr ) --> cReply | NIL\n")
	pw("   VAR cError INIT \"\"  // the text of the last error reply\n\n")
	pw("   METHOD Init( oTransport )\n")
	for _, b := range cmds {
		pw("   METHOD %s( oReq, nErr )\n", camel(b.Name))
	}
	pw("\nENDCLASS\n")
	pw("\nMETHOD Init( oTransport ) CLASS %s\n\n   ::transport := oTransport\n\n   RETURN Self\n", sv.client)
	for _, b := range cmds {
		r := sv.reply[b.Name]
		pw("\nMETHOD %s( oReq, nErr ) CLASS %s\n\n", camel(b.Name), sv.client)
		if r != nil {
			pw("   LOCAL cReply, oReply, hMsg\n\n")
		} else {
			pw("   LOCAL cReply, hMsg\n\n")
		}
		pw("   ::cError := \"\"\n   nErr := KVP_OK\n")
		pw("   cReply := ::transport:Call( oReq:Encode(), @nErr )\n")
		fail := "NIL"
		if r == nil {
			fail = ".F."
		}
		pw("   IF cReply == NIL\n      IF nErr == KVP_OK\n         nErr := KVP_E_CALL\n      ENDIF\n      RETURN %s\n   ENDIF\n", fail)
		pw("   hMsg := KvpParse( cReply, @nErr )\n   IF hMsg == NIL\n      RETURN %s\n   ENDIF\n", fail)
		pw("   IF KvpErrorText( hMsg ) != NIL\n      ::cError := KvpErrorText( hMsg )\n      nErr := KVP_E_REPLY\n      RETURN %s\n   ENDIF\n", fail)
		if r != nil {
			pw("   oReply := %s():New()\n   IF ! oReply:Unpack( hMsg, @nErr )\n      RETURN NIL\n   ENDIF\n\n   RETURN oReply\n", tn.camel(r))
		} else {
			pw("\n   RETURN .T.\n")
		}
	}
}

package gen

import (
	"fmt"
	"strings"

	"github.com/pablo-botella/kvpack/internal/schema"
)

// hbTypes maps a base type to its KVP_* constant, the zero value an
// absent optional field reads as, and the Add function.
var hbTypes = map[string]struct{ kvp, zero, add string }{
	schema.Int32:   {"KVP_INT32", "0", "KvpAddInt32"},
	schema.Int64:   {"KVP_INT64", "0", "KvpAddInt64"},
	schema.TS:      {"KVP_TS", "0", "KvpAddTs"},
	schema.Text:    {"KVP_TEXT", `""`, "KvpAddText"},
	schema.Bin:     {"KVP_BIN", `""`, "KvpAddBin"},
	schema.Msg:     {"KVP_MSG", "NIL", "KvpAddMsg"},
	schema.Bool:    {"KVP_BOOL", ".F.", "KvpAddBool"},
	schema.Float64: {"KVP_FLOAT64", "0", "KvpAddFloat64"},
	schema.DS:      {"KVP_DS", `hb_SToD( "" )`, "KvpAddDs"},
	schema.Rec:     {"KVP_MSG", "NIL", "KvpAddMsg"},
}

// Harbour generates a .ch with the numbers and keys and a .prg with, per
// block, functions over a hash keyed by field name (lists are arrays,
// records are hashes): <P><Block>Pack( hS ) --> writer handle,
// <P><Block>Encode( hS ) --> bytes, <P><Block>Unpack( hMsg, @nErr ) and
// <P><Block>Decode( cBytes, @nErr ) --> hash or NIL. chname is the .ch
// file name the .prg includes.
func Harbour(f *schema.File, prefix, chname string) (ch, prg []byte, err error) {
	if !prefixRe.MatchString(prefix) {
		return nil, nil, fmt.Errorf("Harbour prefix %q: use lowercase letters, digits and _", prefix)
	}
	if err := cCheck(f, prefix); err != nil { // same names, same collisions
		return nil, nil, err
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

	pw("/* %s */\n\n#include \"common.ch\"\n#include \"kvpack.ch\"\n#include \"%s\"\n", header(f), baseName(chname))
	for _, bl := range f.Blocks {
		hbBlock(pw, prefix, P, bl)
	}
	return []byte(cb.String()), []byte(pb.String()), nil
}

func hbName(prefix, block string) string { return camel(prefix) + camel(block) }

func hbBlock(pw func(string, ...any), prefix, P string, bl *schema.Block) {
	fn := hbName(prefix, blockName(bl))
	hasList := false
	for _, fl := range bl.Fields {
		if fl.List {
			hasList = true
		}
	}

	// Pack
	pw("\n/* %s( hS ) --> writer handle for %s %s. */\n", fn+"Pack", bl.Kind, bl.Name)
	pw("FUNCTION %sPack( hS )\n\n   LOCAL hMsg := KvpNew( %s )\n", fn, cNum(P, bl))
	if hasList {
		pw("   LOCAL x\n")
	}
	pw("\n")
	if len(bl.Fields) == 0 {
		pw("   HB_SYMBOL_UNUSED( hS )\n")
	}
	for _, fl := range packOrder(bl) {
		key := cKey(P, bl, fl)
		t := hbTypes[fl.Type]
		get := fmt.Sprintf("hb_HGetDef( hS, %q, %s )", fl.Name, t.zero)
		val := func(x string) string {
			if fl.Type == schema.Rec {
				return hbName(prefix, fl.Record.Name) + "Pack( " + x + " )"
			}
			return x
		}
		switch {
		case fl.List:
			pw("   FOR EACH x IN hb_HGetDef( hS, %q, {} )\n      %s( hMsg, %s, %s )\n   NEXT\n", fl.Name, t.add, key, val("x"))
		case fl.Optional || fl.Type == schema.Msg || fl.Type == schema.Rec:
			pw("   IF ! Empty( %s )\n      %s( hMsg, %s, %s )\n   ENDIF\n", get, t.add, key, val("hS[ \""+fl.Name+"\" ]"))
		default:
			pw("   %s( hMsg, %s, %s )\n", t.add, key, get)
		}
	}
	pw("\n   RETURN hMsg\n")

	// Encode
	pw("\n/* %s( hS ) --> the message bytes. */\n", fn+"Encode")
	pw("FUNCTION %sEncode( hS )\n   RETURN KvpEncode( %sPack( hS ) )\n", fn, fn)

	// Unpack
	pw("\n/* %s( hMsg, @nErr ) --> hash, or NIL with nErr set (KVP_E_MISSING", fn+"Unpack")
	if bl.Kind == schema.Reply {
		pw(", KVP_E_REPLY for an error reply: read it with KvpErrorText")
	}
	pw("). */\n")
	pw("FUNCTION %sUnpack( hMsg, nErr )\n\n   LOCAL hS := { => }\n", fn)
	if hasList {
		pw("   LOCAL aItem, nI\n")
	}
	needSub := false
	for _, fl := range bl.Fields {
		if fl.Type == schema.Rec {
			needSub = true
		}
	}
	if needSub {
		pw("   LOCAL hSub\n")
	}
	pw("\n   nErr := KVP_OK\n")
	if len(bl.Fields) == 0 && bl.Kind != schema.Reply {
		pw("   HB_SYMBOL_UNUSED( hMsg )\n")
	}
	if bl.Kind == schema.Reply {
		pw("   IF KvpErrorText( hMsg ) != NIL\n      nErr := KVP_E_REPLY\n      RETURN NIL\n   ENDIF\n")
	}
	for _, fl := range bl.Fields {
		key := cKey(P, bl, fl)
		t := hbTypes[fl.Type]
		switch {
		case fl.List:
			pw("   hS[ %q ] := {}\n", fl.Name)
			pw("   FOR nI := 1 TO KvpCount( hMsg )\n      aItem := KvpItem( hMsg, nI )\n")
			pw("      IF aItem[ KVP_ITEM_KEY ] == %s .AND. aItem[ KVP_ITEM_TYPE ] == %s\n", key, t.kvp)
			if fl.Type == schema.Rec {
				pw("         hSub := %sUnpack( aItem[ KVP_ITEM_VALUE ], @nErr )\n", hbName(prefix, fl.Record.Name))
				pw("         IF hSub == NIL\n            RETURN NIL\n         ENDIF\n")
				pw("         AAdd( hS[ %q ], hSub )\n", fl.Name)
			} else {
				pw("         AAdd( hS[ %q ], aItem[ KVP_ITEM_VALUE ] )\n", fl.Name)
			}
			pw("      ENDIF\n   NEXT\n")
		default:
			pw("   IF KvpType( hMsg, %s ) == %s\n", key, t.kvp)
			if fl.Type == schema.Rec {
				pw("      hSub := %sUnpack( KvpGet( hMsg, %s ), @nErr )\n", hbName(prefix, fl.Record.Name), key)
				pw("      IF hSub == NIL\n         RETURN NIL\n      ENDIF\n")
				pw("      hS[ %q ] := hSub\n", fl.Name)
			} else {
				pw("      hS[ %q ] := KvpGet( hMsg, %s )\n", fl.Name, key)
			}
			if fl.Required() {
				pw("   ELSE\n      nErr := KVP_E_MISSING\n      RETURN NIL\n")
			} else {
				pw("   ELSE\n      hS[ %q ] := %s\n", fl.Name, t.zero)
			}
			pw("   ENDIF\n")
		}
	}
	pw("\n   RETURN hS\n")

	// Decode
	pw("\n/* %s( cBytes, @nErr ) --> hash, or NIL with nErr set. */\n", fn+"Decode")
	pw("FUNCTION %sDecode( cBytes, nErr )\n\n   LOCAL hMsg := KvpParse( cBytes, @nErr )\n\n", fn)
	pw("   IF hMsg == NIL\n      RETURN NIL\n   ENDIF\n\n   RETURN %sUnpack( hMsg, @nErr )\n", fn)
}

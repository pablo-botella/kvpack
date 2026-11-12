/*
 * test_api.prg - exercises the Harbour classes kvpack generates from
 * test/kvpgen/api.kvp: the canonical replies shared with the Go and C tests,
 * written and read back, plus missing fields, nulls and error replies.
 *   hbmk2 test_api.hbp
 * Exit code 0 when all good; each failure prints its line.
 */
#include "hbclass.ch"
#include "kvpack.ch"
#include "api_kvp.ch"

STATIC s_nFail := 0

#xcommand CHECK <expr> => IF !( <expr> ) ; s_nFail++ ; ? "FAIL line", ProcLine(), <"expr"> ; ENDIF

/* The canonical reply (see canonicalReplyHex in test/kvpgen/kvpgen_test.go). */
#define CANONICAL_HEX ;
   "4b5650010100000005000000400100000c0f37fa0100040003000000616e6e020003000800000000f1536500000000" + ;
   "050006001e0000004b56500165000000010000000a000000e6db35f00100040002000000646904000600670000004b" + ;
   "56500164000000050000005300000022dac938010001000400000007000000020004000100000061070006001e0000" + ;
   "004b56500165000000010000000a00000055d90c4f0100040002000000626f06000100040000000100000006000100" + ;
   "040000000200000004000600880000004b565001640000000700000074000000411e320d0100010004000000080000" + ;
   "0002000400010000006203000200080000000500000000000000070006001e0000004b56500165000000010000000a" + ;
   "000000455dc3a201000400020000006379080007000100000001090008000800000000000000000004400a00090008" + ;
   "0000003230323631313131"

/* The canonical query reply (see canonicalQueryHex in test/kvpgen/kvpgen_test.go). */
#define QUERY_HEX ;
   "4b5650010300000006000000930000000704f72e01000100040000000700000002000a0000000000040006001e0000004b" + ;
   "56500165000000010000000a000000e6db35f00100040002000000646905000b001e000000020000000100020000006964" + ;
   "000000000400040000006e616d650000000005000c001300000001000400000001000000040003000000416e6105000c00" + ;
   "10000000010004000000020000000a0000000000"

PROCEDURE Main()
   TestWriteCanonical()
   TestReadCanonical()
   TestQuery()
   TestErrors()
   TestEveryWrite()
   TestEveryRead()
   TestEveryKeyByKey()
   TestServer()
   TestClient()
   IF s_nFail > 0
      ? s_nFail, "failure(s)"
      ErrorLevel( 1 )
   ELSE
      ? "kvpack generated Harbour: all good"
   ENDIF
   ?
   RETURN

STATIC FUNCTION Person( cName )
   LOCAL o := ApiPerson():New()
   o:name := cName
   RETURN o

STATIC FUNCTION CanonicalReply()
   LOCAL oR := ApiLoginReply():New(), oEv

   oR:name    := "ann"
   oR:expires := 1700000000
   oR:owner   := Person( "di" )

   oEv := ApiEvent():New()                 // a record element is an object of its own class
   oEv:id    := 7
   oEv:title := "a"
   oEv:codes := { 1, 2 }
   oEv:who   := Person( "bo" )
   AAdd( oR:events, oEv )

   oEv := ApiEvent():New()
   oEv:id     := 8
   oEv:title  := "b"
   oEv:big    := 5
   oEv:who    := Person( "cy" )
   oEv:active := .T.
   oEv:score  := 2.5
   oEv:day    := hb_SToD( "20261111" )
   AAdd( oR:events, oEv )

   RETURN oR

STATIC PROCEDURE TestWriteCanonical()
   LOCAL oA := ApiLoginReply():New(), oB := ApiLoginReply():New()

   CHECK Lower( hb_StrToHex( CanonicalReply():Encode() ) ) == CANONICAL_HEX

   /* every object has its own variables: a list is not shared between two */
   AAdd( oA:events, ApiEvent():New() )
   CHECK Len( oA:events ) == 1 .AND. Len( oB:events ) == 0
   CHECK oA:name == "" .AND. oA:expires == 0 .AND. oA:level == 0 .AND. oA:owner == NIL
   RETURN

STATIC PROCEDURE TestReadCanonical()
   LOCAL nErr, oR := ApiLoginReply():New()

   CHECK oR:Decode( hb_HexToStr( CANONICAL_HEX ), @nErr ) .AND. nErr == KVP_OK
   CHECK oR:name == "ann"
   CHECK oR:expires == 1700000000
   CHECK oR:level == 0
   CHECK oR:owner:ClassName() == "APIPERSON" .AND. oR:owner:name == "di"
   CHECK Len( oR:events ) == 2
   IF Len( oR:events ) != 2
      RETURN
   ENDIF
   CHECK oR:events[ 1 ]:ClassName() == "APIEVENT"
   CHECK oR:events[ 1 ]:id == 7 .AND. oR:events[ 1 ]:title == "a"
   CHECK oR:events[ 1 ]:big == 0 .AND. oR:events[ 1 ]:blob == "" .AND. oR:events[ 1 ]:raw == NIL
   CHECK Len( oR:events[ 1 ]:codes ) == 2 .AND. oR:events[ 1 ]:codes[ 2 ] == 2
   CHECK oR:events[ 1 ]:who:name == "bo"
   CHECK oR:events[ 2 ]:id == 8 .AND. oR:events[ 2 ]:big == 5
   CHECK oR:events[ 2 ]:who:name == "cy"
   CHECK oR:events[ 1 ]:active == .F. .AND. oR:events[ 1 ]:score == 0 .AND. Empty( oR:events[ 1 ]:day )
   CHECK oR:events[ 2 ]:active == .T. .AND. oR:events[ 2 ]:score == 2.5
   CHECK DToS( oR:events[ 2 ]:day ) == "20261111"

   /* what was decoded encodes back to the same bytes */
   CHECK Lower( hb_StrToHex( oR:Encode() ) ) == CANONICAL_HEX
   RETURN

STATIC FUNCTION Rows()
   LOCAL oSet := KvpTupleSet():New()
   oSet:AddField( "id", KVP_INT32 )
   oSet:AddField( "name", KVP_TEXT )
   oSet:AddRow( { 1, "Ana" } )
   oSet:AddRow( { 2, NIL } )
   RETURN oSet

STATIC FUNCTION Query( xNote, xLimit )
   LOCAL oR := ApiQueryReply():New()
   oR:code   := 1
   oR:note   := xNote
   oR:limit  := xLimit
   oR:author := Person( "di" )
   oR:rows   := Rows()
   RETURN oR

STATIC PROCEDURE TestQuery()
   LOCAL nErr, oR, hMsg, cBytes

   /* a nullable field sent as a null, an optional nullable one that is null and so left out, a record and a tupleset */
   oR := Query( NIL, NIL )
   oR:code := 7
   CHECK Lower( hb_StrToHex( oR:Encode() ) ) == QUERY_HEX

   oR := ApiQueryReply():New()
   CHECK oR:Decode( hb_HexToStr( QUERY_HEX ), @nErr ) .AND. nErr == KVP_OK
   CHECK oR:code == 7
   CHECK oR:note == NIL
   CHECK oR:limit == NIL                               // an optional nullable that did not arrive is null
   CHECK oR:author:name == "di"
   CHECK oR:rows:ClassName() == "KVPTUPLESET"
   CHECK Len( oR:rows:aFields ) == 2 .AND. oR:rows:aFields[ 2 ][ KVP_FIELD_NAME ] == "name"
   CHECK Len( oR:rows:aRows ) == 2
   CHECK oR:rows:aRows[ 1 ][ 1 ] == 1 .AND. oR:rows:aRows[ 1 ][ 2 ] == "Ana"
   CHECK oR:rows:aRows[ 2 ][ 1 ] == 2 .AND. oR:rows:aRows[ 2 ][ oR:rows:FieldPos( "name" ) ] == NIL
   CHECK oR:extra:ClassName() == "KVPTUPLESET" .AND. Len( oR:extra:aFields ) == 0   // it did not arrive: empty
   CHECK Lower( hb_StrToHex( oR:Encode() ) ) == QUERY_HEX

   /* nulls where they may come, rows with no tuples, the optional tupleset present */
   hMsg := KvpNew( API_CMD_QUERY )
   KvpAddInt32( hMsg, API_QUERY_REPLY_CODE, 0 )
   KvpAddNull( hMsg, API_QUERY_REPLY_NOTE )
   KvpAddNull( hMsg, API_QUERY_REPLY_LIMIT )
   KvpAddNull( hMsg, API_QUERY_REPLY_AUTHOR )
   KvpAddTupleDef( hMsg, API_QUERY_REPLY_ROWS, { { "n", KVP_INT32 } } )
   KvpAddTupleDef( hMsg, API_QUERY_REPLY_EXTRA, { { "x", KVP_TEXT } } )
   oR := ApiQueryReply():New()
   CHECK oR:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_OK
   CHECK oR:note == NIL .AND. oR:limit == NIL .AND. oR:author == NIL
   CHECK Len( oR:rows:aRows ) == 0 .AND. Len( oR:extra:aFields ) == 1

   /* an optional nullable is left out when NIL, and written when not, whatever its value */
   hMsg := KvpParse( Query( "hi", NIL ):Encode() )
   CHECK hMsg != NIL .AND. KvpType( hMsg, API_QUERY_REPLY_LIMIT ) == 0 .AND. KvpGet( hMsg, API_QUERY_REPLY_NOTE ) == "hi"
   cBytes := Query( "", 0 ):Encode()
   hMsg := KvpParse( cBytes )
   CHECK hMsg != NIL .AND. KvpType( hMsg, API_QUERY_REPLY_LIMIT ) == KVP_INT32 .AND. KvpGet( hMsg, API_QUERY_REPLY_LIMIT ) == 0
   CHECK hMsg != NIL .AND. KvpType( hMsg, API_QUERY_REPLY_NOTE ) == KVP_TEXT
   oR := ApiQueryReply():New()
   CHECK oR:Decode( cBytes, @nErr ) .AND. oR:limit == 0 .AND. oR:note == ""

   /* a new object: nothing null, so a nullable goes as an empty value, the optional nullable as a zero, the record with
      its fields empty and the tupleset as a definition of no fields; the optional tupleset is left out */
   oR := ApiQueryReply():New()
   hMsg := KvpParse( oR:Encode() )
   CHECK hMsg != NIL .AND. KvpCount( hMsg ) == 5
   CHECK hMsg != NIL .AND. KvpType( hMsg, API_QUERY_REPLY_NOTE ) == KVP_TEXT .AND. KvpType( hMsg, API_QUERY_REPLY_LIMIT ) == KVP_INT32
   CHECK hMsg != NIL .AND. KvpType( hMsg, API_QUERY_REPLY_AUTHOR ) == KVP_MSG .AND. KvpType( hMsg, API_QUERY_REPLY_ROWS ) == KVP_TUPLEDEF
   CHECK oR:Decode( oR:Encode(), @nErr ) .AND. Len( oR:rows:aFields ) == 0 .AND. oR:author:name == ""

   /* what is not nullable refuses a null, and the tupleset must come */
   hMsg := KvpNew( API_CMD_QUERY )
   KvpAddNull( hMsg, API_QUERY_REPLY_CODE )
   CHECK ! oR:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_MISSING
   hMsg := KvpNew( API_CMD_QUERY )
   KvpAddInt32( hMsg, API_QUERY_REPLY_CODE, 0 )
   KvpAddNull( hMsg, API_QUERY_REPLY_NOTE )
   KvpAddNull( hMsg, API_QUERY_REPLY_AUTHOR )
   CHECK ! oR:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_MISSING
   RETURN

STATIC PROCEDURE TestErrors()
   LOCAL nErr, hMsg, oL := ApiLogin():New(), oR := ApiLoginReply():New()

   /* password missing */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, API_LOGIN_USER, "bob" )
   CHECK ! oL:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_MISSING

   /* password of another type */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, API_LOGIN_USER, "bob" )
   KvpAddInt32( hMsg, API_LOGIN_PASSWORD, 5 )
   CHECK ! oL:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_MISSING

   /* optional left out: user and password only; then a list, where spaces are a value like any other */
   oL := ApiLogin():New()
   oL:user     := "bob"
   oL:password := "x"
   CHECK Len( oL:Encode() ) == 20 + 2 * 8 + 4
   oL:tags := { "one", "", "  " }
   CHECK Len( oL:Encode() ) == 20 + 5 * 8 + 4 + 5
   CHECK oL:Decode( oL:Encode(), @nErr ) .AND. Len( oL:tags ) == 3 .AND. oL:tags[ 3 ] == "  " .AND. oL:when == 0

   /* Pack and Unpack: the same over a handle */
   hMsg := KvpParse( KvpEncode( oL:Pack() ) )
   oL := ApiLogin():New()
   CHECK oL:Unpack( hMsg, @nErr ) .AND. oL:user == "bob" .AND. nErr == KVP_OK

   /* error reply */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, KVP_KEY_ERROR, "wrong password" )
   CHECK ! oR:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_REPLY

   /* not a message */
   CHECK ! oL:Decode( "hello", @nErr ) .AND. nErr == KVP_E_TRUNCATED
   RETURN

/* --- command every ----------------------------------------------------------
 * Each type in each form a field can take: required (r_), optional (o_),
 * nullable (n_), optional nullable (on_) and list (l_), plus a required and
 * an optional tupleset. Three messages of it, the same in the Go test
 * (everyFullHex, everyNullsHex, everyZerosHex in kvpgen_test.go) and in the
 * C one:
 *   full   every field with a value, two elements per list, a tupleset with
 *          a field of each type, one tuple with values and one all null
 *   zeros  what an object nobody touched writes, the same in the three
 *          languages: every field that always travels as its empty value (a
 *          zero, no bytes, a message of command 0 and no items, a record of
 *          empty fields, a tuple definition of no fields), nothing of the
 *          optional fields nor the lists
 *   nulls  the same with every field that can be null set to NIL: the
 *          nullable ones go as nulls, the optional nullable ones are left out
 */
STATIC FUNCTION EveryFullHex()
   RETURN ;
   "4b56500104000000400000003b0500007860084f0100010004000000ffffffff0200020008000000000000000001000003" + ;
   "0003000800000000f153650000000004000400040000007465787405000500030000000001ff0600070001000000010700" + ;
   "08000800000000000000000004400800090008000000323032363131313209000600140000004b56500109000000000000" + ;
   "0000000000000000000a0006001d0000004b5650016500000001000000090000005e1d56860100040001000000720b0001" + ;
   "00040000000b0000000c000200080000000c000000000000000d000300080000000df15365000000000e00040001000000" + ;
   "6f0f000500010000000f100007000100000001110008000800000000000000008031401200090008000000323032363031" + ;
   "313813000600140000004b56500113000000000000000000000000000000140006001d0000004b56500165000000010000" + ;
   "0009000000877150e501000400010000006f15000100040000001500000016000200080000001600000000000000170003" + ;
   "000800000017f153650000000018000400010000006e1900050001000000191a00070001000000011b0008000800000000" + ;
   "00000000803b401c0009000800000032303236303132381d000600140000004b5650011d00000000000000000000000000" + ;
   "00001e0006001d0000004b5650016500000001000000090000001141579201000400010000006e1f000100040000001f00" + ;
   "000020000200080000002000000000000000210003000800000021f153650000000022000400020000006f6e2300050001" + ;
   "0000002324000700010000000125000800080000000000000000c042402600090008000000323032363032313827000600" + ;
   "140000004b56500127000000000000000000000000000000280006001e0000004b56500165000000010000000a0000008e" + ;
   "97a58d01000400020000006f6e2900010004000000010000002900010004000000feffffff2a0002000800000003000000" + ;
   "000000002a00020008000000fcffffffffffffff2b000300080000002bf15365000000002b000300080000000000000000" + ;
   "0000002c00040001000000612c000400000000002d0005000200000001022d000500000000002e00070001000000012e00" + ;
   "070001000000002f00080008000000000000000000e03f2f00080008000000000000000000f8bf30000900080000003230" + ;
   "3236303130313000090008000000202020202020202031000600140000004b565001010000000000000000000000000000" + ;
   "0031000600140000004b56500102000000000000000000000000000000320006001d0000004b5650016500000001000000" + ;
   "0900000040f48366010004000100000078320006001d0000004b565001650000000100000009000000d6c4841101000400" + ;
   "010000007933000b006d0000000900000001000100000069000000000200010000006a0000000003000100000074000000" + ;
   "0004000100000073060000006c656e3d343005000100000062000000000600010000006d000000000700010000006b0000" + ;
   "00000800010000006600000000090001000000640000000033000c00730000000100040000000100000002000800000002" + ;
   "0000000000000003000800000003f1536500000000040003000000416e61050001000000040600140000004b5650010500" + ;
   "0000000000000000000000000000070001000000010800080000000000000000001a400900080000003230323630373037" + ;
   "33000c00360000000a00000000000a00000000000a00000000000a00000000000a00000000000a00000000000a00000000" + ;
   "000a00000000000a000000000034000b000f000000010000000100010000006e00000000"

STATIC FUNCTION EveryNullsHex()
   RETURN ;
   "4b565001040000001500000001010000ecb25ee10100010004000000000000000200020008000000000000000000000003" + ;
   "00030008000000000000000000000004000400000000000500050000000000060007000100000000070008000800000000" + ;
   "000000000000000800090008000000202020202020202009000600140000004b5650010000000000000000000000000000" + ;
   "00000a0006001c0000004b565001650000000100000008000000e19d1932010004000000000015000a000000000016000a" + ;
   "000000000017000a000000000018000a000000000019000a00000000001a000a00000000001b000a00000000001c000a00" + ;
   "000000001d000a00000000001e000a000000000033000b000400000000000000"

STATIC FUNCTION EveryZerosHex()
   RETURN ;
   "4b565001040000001f000000fb010000d3e7d1a40100010004000000000000000200020008000000000000000000000003" + ;
   "00030008000000000000000000000004000400000000000500050000000000060007000100000000070008000800000000" + ;
   "000000000000000800090008000000202020202020202009000600140000004b5650010000000000000000000000000000" + ;
   "00000a0006001c0000004b565001650000000100000008000000e19d193201000400000000001500010004000000000000" + ;
   "00160002000800000000000000000000001700030008000000000000000000000018000400000000001900050000000000" + ;
   "1a00070001000000001b0008000800000000000000000000001c0009000800000020202020202020201d00060014000000" + ;
   "4b565001000000000000000000000000000000001e0006001c0000004b565001650000000100000008000000e19d193201" + ;
   "000400000000001f0001000400000000000000200002000800000000000000000000002100030008000000000000000000" + ;
   "00002200040000000000230005000000000024000700010000000025000800080000000000000000000000260009000800" + ;
   "0000202020202020202027000600140000004b56500100000000000000000000000000000000280006001c0000004b5650" + ;
   "01650000000100000008000000e19d1932010004000000000033000b000400000000000000"

STATIC FUNCTION EveryFull()
   LOCAL o := ApiEvery():New(), oSet

   /* required */
   o:r_int32   := -1
   o:r_int64   := 1099511627776                   // 2 ^ 40
   o:r_ts      := 1700000000
   o:r_text    := "text"
   o:r_bin     := hb_BChar( 0 ) + hb_BChar( 1 ) + hb_BChar( 255 )
   o:r_bool    := .T.
   o:r_float64 := 2.5
   o:r_ds      := hb_SToD( "20261112" )
   o:r_msg     := KvpNew( 9 )
   o:r_rec     := Person( "r" )

   /* optional */
   o:o_int32   := 11
   o:o_int64   := 12
   o:o_ts      := 1700000013
   o:o_text    := "o"
   o:o_bin     := hb_BChar( 15 )
   o:o_bool    := .T.
   o:o_float64 := 17.5
   o:o_ds      := hb_SToD( "20260118" )
   o:o_msg     := KvpNew( 19 )
   o:o_rec     := Person( "o" )

   /* nullable */
   o:n_int32   := 21
   o:n_int64   := 22
   o:n_ts      := 1700000023
   o:n_text    := "n"
   o:n_bin     := hb_BChar( 25 )
   o:n_bool    := .T.
   o:n_float64 := 27.5
   o:n_ds      := hb_SToD( "20260128" )
   o:n_msg     := KvpNew( 29 )
   o:n_rec     := Person( "n" )

   /* optional nullable */
   o:on_int32   := 31
   o:on_int64   := 32
   o:on_ts      := 1700000033
   o:on_text    := "on"
   o:on_bin     := hb_BChar( 35 )
   o:on_bool    := .T.
   o:on_float64 := 37.5
   o:on_ds      := hb_SToD( "20260218" )
   o:on_msg     := KvpNew( 39 )
   o:on_rec     := Person( "on" )

   /* lists: two elements each, the second one empty where the type has an empty */
   o:l_int32   := { 1, -2 }
   o:l_int64   := { 3, -4 }
   o:l_ts      := { 1700000043, 0 }
   o:l_text    := { "a", "" }
   o:l_bin     := { hb_BChar( 1 ) + hb_BChar( 2 ), "" }
   o:l_bool    := { .T., .F. }
   o:l_float64 := { 0.5, -1.5 }
   o:l_ds      := { hb_SToD( "20260101" ), hb_SToD( "" ) }
   o:l_msg     := { KvpNew( 1 ), KvpNew( 2 ) }
   o:l_rec     := { Person( "x" ), Person( "y" ) }

   /* a tupleset with a field of each type: a tuple with values, one all null */
   oSet := KvpTupleSet():New()
   oSet:AddField( "i", KVP_INT32 )
   oSet:AddField( "j", KVP_INT64 )
   oSet:AddField( "t", KVP_TS )
   oSet:AddField( "s", KVP_TEXT, "len=40" )
   oSet:AddField( "b", KVP_BIN )
   oSet:AddField( "m", KVP_MSG )
   oSet:AddField( "k", KVP_BOOL )
   oSet:AddField( "f", KVP_FLOAT64 )
   oSet:AddField( "d", KVP_DS )
   oSet:AddRow( { 1, 2, 1700000003, "Ana", hb_BChar( 4 ), KvpNew( 5 ), .T., 6.5, hb_SToD( "20260707" ) } )
   oSet:AddRow( Array( 9 ) )
   o:r_set := oSet

   o:o_set := KvpTupleSet():New()
   o:o_set:AddField( "n", KVP_INT32 )

   RETURN o

/* EveryZeros: an object nobody touched. Nothing null, everything empty. */
STATIC FUNCTION EveryZeros()
   RETURN ApiEvery():New()

/* EveryNulls: EveryZeros with every field that can be null set to NIL. */
STATIC FUNCTION EveryNulls()
   LOCAL o := EveryZeros()

   o:n_int32 := o:n_int64 := o:n_ts := o:n_text := o:n_bin := NIL
   o:n_bool := o:n_float64 := o:n_ds := o:n_msg := o:n_rec := NIL
   o:on_int32 := o:on_int64 := o:on_ts := o:on_text := o:on_bin := NIL
   o:on_bool := o:on_float64 := o:on_ds := o:on_msg := o:on_rec := NIL

   RETURN o

/* U16 and U32: a number as the little-endian bytes of the format. */
STATIC FUNCTION U16( n )
   RETURN hb_BChar( n % 256 ) + hb_BChar( Int( n / 256 ) )

STATIC FUNCTION U32( n )
   RETURN U16( n % 65536 ) + U16( Int( n / 65536 ) )

/* Items walks the bytes of a message: { { nKey, nType, cItem }, ... }, cItem
 * the whole item, its 8 bytes of key, type and size included. */
STATIC FUNCTION Items( cBytes )
   LOCAL aItems := {}, nPos := 21, nSize

   DO WHILE nPos + 7 <= hb_BLen( cBytes )
      nSize := Bin2L( hb_BSubStr( cBytes, nPos + 4, 4 ) )
      AAdd( aItems, { Bin2W( hb_BSubStr( cBytes, nPos, 2 ) ), Bin2W( hb_BSubStr( cBytes, nPos + 2, 2 ) ), hb_BSubStr( cBytes, nPos, 8 + nSize ) } )
      nPos += 8 + nSize
   ENDDO

   RETURN aItems

/* KeysOf lists the keys of a message, in order, a null as its negative: "1,2,-3". */
STATIC FUNCTION KeysOf( cBytes )
   LOCAL cKeys := "", aItem

   FOR EACH aItem IN Items( cBytes )
      cKeys += iif( Empty( cKeys ), "", "," ) + hb_ntos( iif( aItem[ 2 ] == KVP_NULL, -aItem[ 1 ], aItem[ 1 ] ) )
   NEXT

   RETURN cKeys

STATIC FUNCTION Span( nFrom, nTo, nSign )
   LOCAL cKeys := "", nKey

   FOR nKey := nFrom TO nTo
      cKeys += iif( nKey == nFrom, "", "," ) + hb_ntos( iif( nSign == NIL, nKey, nSign * nKey ) )
   NEXT

   RETURN cKeys

/* The three messages are written as the Go and C code writes them, and each
 * carries the keys the rule of its form says. */
STATIC PROCEDURE TestEveryWrite()
   LOCAL cBytes, cKeys, nKey, o

   /* full: everything, two elements per list, a definition and two tuples, a definition */
   cBytes := EveryFull():Encode()
   CHECK Lower( hb_StrToHex( cBytes ) ) == EveryFullHex()
   cKeys := Span( 1, 40 )
   FOR nKey := 41 TO 50
      cKeys += "," + hb_ntos( nKey ) + "," + hb_ntos( nKey )
   NEXT
   CHECK KeysOf( cBytes ) == cKeys + ",51,51,51,52"

   /* nulls: the required fields, the nullable ones as nulls, and nothing of the optional, the optional nullable nor the lists */
   cBytes := EveryNulls():Encode()
   CHECK Lower( hb_StrToHex( cBytes ) ) == EveryNullsHex()
   CHECK KeysOf( cBytes ) == Span( 1, 10 ) + "," + Span( 21, 30, -1 ) + ",51"

   /* zeros: an optional field at zero is left out; a nullable one and an optional nullable one at zero are values, and travel */
   cBytes := EveryZeros():Encode()
   CHECK Lower( hb_StrToHex( cBytes ) ) == EveryZerosHex()
   CHECK KeysOf( cBytes ) == Span( 1, 10 ) + "," + Span( 21, 40 ) + ",51"

   /* a new object has what always travels ready to fill, each object its own */
   o := ApiEvery():New()
   CHECK o:r_rec:ClassName() == "APIPERSON" .AND. o:n_rec:ClassName() == "APIPERSON" .AND. o:on_rec:ClassName() == "APIPERSON"
   CHECK HB_ISPOINTER( o:r_msg ) .AND. HB_ISPOINTER( o:n_msg ) .AND. HB_ISPOINTER( o:on_msg )
   CHECK o:r_set:ClassName() == "KVPTUPLESET" .AND. o:o_set:ClassName() == "KVPTUPLESET"
   CHECK o:o_rec == NIL .AND. o:o_msg == NIL                // an optional one: there is none
   o:r_rec:name := "mine"
   CHECK ApiEvery():New():r_rec:name == ""

   /* where NIL is no null it is the empty value: the same message */
   o := ApiEvery():New()
   o:r_msg := NIL
   o:r_rec := NIL
   o:r_set := NIL
   o:o_set := NIL
   CHECK Lower( hb_StrToHex( o:Encode() ) ) == EveryZerosHex()
   RETURN

/* The three messages are read and every field looked at. */
STATIC PROCEDURE TestEveryRead()
   LOCAL nErr, o := ApiEvery():New(), aRow, x

   /* full */
   CHECK o:Decode( hb_HexToStr( EveryFullHex() ), @nErr ) .AND. nErr == KVP_OK

   CHECK o:r_int32 == -1 .AND. o:r_int64 == 1099511627776 .AND. o:r_ts == 1700000000 .AND. o:r_text == "text"
   CHECK o:r_bin == hb_BChar( 0 ) + hb_BChar( 1 ) + hb_BChar( 255 ) .AND. o:r_bool == .T. .AND. o:r_float64 == 2.5
   CHECK DToS( o:r_ds ) == "20261112" .AND. KvpCommand( o:r_msg ) == 9 .AND. o:r_rec:name == "r"

   CHECK o:o_int32 == 11 .AND. o:o_int64 == 12 .AND. o:o_ts == 1700000013 .AND. o:o_text == "o"
   CHECK o:o_bin == hb_BChar( 15 ) .AND. o:o_bool == .T. .AND. o:o_float64 == 17.5
   CHECK DToS( o:o_ds ) == "20260118" .AND. KvpCommand( o:o_msg ) == 19 .AND. o:o_rec:name == "o"

   CHECK o:n_int32 == 21 .AND. o:n_int64 == 22 .AND. o:n_ts == 1700000023 .AND. o:n_text == "n"
   CHECK o:n_bin == hb_BChar( 25 ) .AND. o:n_bool == .T. .AND. o:n_float64 == 27.5
   CHECK DToS( o:n_ds ) == "20260128" .AND. KvpCommand( o:n_msg ) == 29 .AND. o:n_rec:name == "n"

   CHECK o:on_int32 == 31 .AND. o:on_int64 == 32 .AND. o:on_ts == 1700000033 .AND. o:on_text == "on"
   CHECK o:on_bin == hb_BChar( 35 ) .AND. o:on_bool == .T. .AND. o:on_float64 == 37.5
   CHECK DToS( o:on_ds ) == "20260218" .AND. KvpCommand( o:on_msg ) == 39 .AND. o:on_rec:name == "on"

   CHECK Len( o:l_int32 ) == 2 .AND. o:l_int32[ 1 ] == 1 .AND. o:l_int32[ 2 ] == -2
   CHECK Len( o:l_int64 ) == 2 .AND. o:l_int64[ 1 ] == 3 .AND. o:l_int64[ 2 ] == -4
   CHECK Len( o:l_ts ) == 2 .AND. o:l_ts[ 1 ] == 1700000043 .AND. o:l_ts[ 2 ] == 0
   CHECK Len( o:l_text ) == 2 .AND. o:l_text[ 1 ] == "a" .AND. o:l_text[ 2 ] == ""
   CHECK Len( o:l_bin ) == 2 .AND. o:l_bin[ 1 ] == hb_BChar( 1 ) + hb_BChar( 2 ) .AND. o:l_bin[ 2 ] == ""
   CHECK Len( o:l_bool ) == 2 .AND. o:l_bool[ 1 ] == .T. .AND. o:l_bool[ 2 ] == .F.
   CHECK Len( o:l_float64 ) == 2 .AND. o:l_float64[ 1 ] == 0.5 .AND. o:l_float64[ 2 ] == -1.5
   CHECK Len( o:l_ds ) == 2 .AND. DToS( o:l_ds[ 1 ] ) == "20260101" .AND. HB_ISDATE( o:l_ds[ 2 ] ) .AND. Empty( o:l_ds[ 2 ] )
   CHECK Len( o:l_msg ) == 2 .AND. KvpCommand( o:l_msg[ 1 ] ) == 1 .AND. KvpCommand( o:l_msg[ 2 ] ) == 2
   CHECK Len( o:l_rec ) == 2 .AND. o:l_rec[ 1 ]:ClassName() == "APIPERSON" .AND. o:l_rec[ 1 ]:name == "x" .AND. o:l_rec[ 2 ]:name == "y"

   /* the tuplesets: a field of each type, a tuple with values and one all null */
   CHECK Len( o:r_set:aFields ) == 9 .AND. Len( o:r_set:aRows ) == 2
   CHECK o:r_set:aFields[ 4 ][ KVP_FIELD_NAME ] == "s" .AND. o:r_set:aFields[ 4 ][ KVP_FIELD_TYPE ] == KVP_TEXT
   CHECK o:r_set:aFields[ 4 ][ KVP_FIELD_META ] == "len=40" .AND. o:r_set:aFields[ 1 ][ KVP_FIELD_META ] == ""
   aRow := o:r_set:aRows[ 1 ]
   CHECK aRow[ 1 ] == 1 .AND. aRow[ 2 ] == 2 .AND. aRow[ 3 ] == 1700000003 .AND. aRow[ 4 ] == "Ana" .AND. aRow[ 5 ] == hb_BChar( 4 )
   CHECK KvpCommand( aRow[ 6 ] ) == 5 .AND. aRow[ 7 ] == .T. .AND. aRow[ 8 ] == 6.5 .AND. DToS( aRow[ 9 ] ) == "20260707"
   CHECK Len( o:r_set:aRows[ 2 ] ) == 9
   FOR EACH x IN o:r_set:aRows[ 2 ]
      CHECK x == NIL
   NEXT
   CHECK Len( o:o_set:aFields ) == 1 .AND. Len( o:o_set:aRows ) == 0

   /* what was read writes the same bytes again */
   CHECK Lower( hb_StrToHex( o:Encode() ) ) == EveryFullHex()

   /* nulls: an optional field that did not arrive reads empty; a nullable one that came as a null and an optional
      nullable one that did not arrive read NIL */
   o := ApiEvery():New()
   CHECK o:Decode( hb_HexToStr( EveryNullsHex() ), @nErr ) .AND. nErr == KVP_OK
   CHECK o:r_int32 == 0 .AND. o:r_int64 == 0 .AND. o:r_ts == 0 .AND. o:r_text == "" .AND. o:r_bin == ""
   CHECK o:r_bool == .F. .AND. o:r_float64 == 0 .AND. HB_ISDATE( o:r_ds ) .AND. Empty( o:r_ds )
   CHECK KvpCommand( o:r_msg ) == 0 .AND. o:r_rec:name == ""
   CHECK o:o_int32 == 0 .AND. o:o_int64 == 0 .AND. o:o_ts == 0 .AND. o:o_text == "" .AND. o:o_bin == ""
   CHECK o:o_bool == .F. .AND. o:o_float64 == 0 .AND. HB_ISDATE( o:o_ds ) .AND. Empty( o:o_ds )
   CHECK o:o_msg == NIL .AND. o:o_rec == NIL
   CHECK o:n_int32 == NIL .AND. o:n_int64 == NIL .AND. o:n_ts == NIL .AND. o:n_text == NIL .AND. o:n_bin == NIL
   CHECK o:n_bool == NIL .AND. o:n_float64 == NIL .AND. o:n_ds == NIL .AND. o:n_msg == NIL .AND. o:n_rec == NIL
   CHECK o:on_int32 == NIL .AND. o:on_int64 == NIL .AND. o:on_ts == NIL .AND. o:on_text == NIL .AND. o:on_bin == NIL
   CHECK o:on_bool == NIL .AND. o:on_float64 == NIL .AND. o:on_ds == NIL .AND. o:on_msg == NIL .AND. o:on_rec == NIL
   CHECK Len( o:l_int32 ) == 0 .AND. Len( o:l_int64 ) == 0 .AND. Len( o:l_ts ) == 0 .AND. Len( o:l_text ) == 0 .AND. Len( o:l_bin ) == 0
   CHECK Len( o:l_bool ) == 0 .AND. Len( o:l_float64 ) == 0 .AND. Len( o:l_ds ) == 0 .AND. Len( o:l_msg ) == 0 .AND. Len( o:l_rec ) == 0
   /* the tupleset of no fields arrived; the optional one did not, and reads the same */
   CHECK Len( o:r_set:aFields ) == 0 .AND. Len( o:r_set:aRows ) == 0
   CHECK Len( o:o_set:aFields ) == 0 .AND. Len( o:o_set:aRows ) == 0
   CHECK Lower( hb_StrToHex( o:Encode() ) ) == EveryNullsHex()

   /* zeros: a zero that arrives is a value, not a null */
   o := ApiEvery():New()
   CHECK o:Decode( hb_HexToStr( EveryZerosHex() ), @nErr ) .AND. nErr == KVP_OK
   CHECK HB_ISNUMERIC( o:n_int32 ) .AND. o:n_int32 == 0 .AND. HB_ISNUMERIC( o:n_int64 ) .AND. HB_ISNUMERIC( o:n_ts ) .AND. HB_ISNUMERIC( o:n_float64 )
   CHECK HB_ISSTRING( o:n_text ) .AND. o:n_text == "" .AND. HB_ISSTRING( o:n_bin ) .AND. HB_ISLOGICAL( o:n_bool ) .AND. ! o:n_bool
   CHECK HB_ISDATE( o:n_ds ) .AND. Empty( o:n_ds ) .AND. KvpCommand( o:n_msg ) == 0 .AND. o:n_rec:name == ""
   CHECK HB_ISNUMERIC( o:on_int32 ) .AND. o:on_int32 == 0 .AND. HB_ISNUMERIC( o:on_int64 ) .AND. HB_ISNUMERIC( o:on_ts ) .AND. HB_ISNUMERIC( o:on_float64 )
   CHECK HB_ISSTRING( o:on_text ) .AND. o:on_text == "" .AND. HB_ISSTRING( o:on_bin ) .AND. HB_ISLOGICAL( o:on_bool ) .AND. ! o:on_bool
   CHECK HB_ISDATE( o:on_ds ) .AND. Empty( o:on_ds ) .AND. KvpCommand( o:on_msg ) == 0 .AND. o:on_rec:name == ""
   CHECK Lower( hb_StrToHex( o:Encode() ) ) == EveryZerosHex()
   RETURN

/* How TestEveryKeyByKey changes one key of the full message. */
#define DROP_KEY  1     // the key does not arrive
#define NULL_KEY  2     // it arrives as a null
#define OTHER_KEY 3     // it arrives with another type

/* Change returns a message with the items of nKey dropped, or the first of
 * them replaced by a null or by a value of another type. */
STATIC FUNCTION Change( cBytes, nKey, nHow )
   LOCAL cData := "", nItems := 0, lDone := .F., aItem

   FOR EACH aItem IN Items( cBytes )
      IF aItem[ 1 ] != nKey
         cData += aItem[ 3 ]
         nItems++
         LOOP
      ENDIF
      IF ! lDone .AND. nHow == NULL_KEY
         cData += U16( nKey ) + U16( KVP_NULL ) + U32( 0 )
         nItems++
      ELSEIF ! lDone .AND. nHow == OTHER_KEY
         IF aItem[ 2 ] == KVP_TEXT
            cData += U16( nKey ) + U16( KVP_INT32 ) + U32( 4 ) + U32( 1 )
         ELSE
            cData += U16( nKey ) + U16( KVP_TEXT ) + U32( 1 ) + "x"
         ENDIF
         nItems++
      ENDIF
      lDone := .T.
   NEXT

   /* the header again: the magic, version and command stay; the count, the size and the crc32 of the data change */
   RETURN hb_BLeft( cBytes, 8 ) + U32( nItems ) + U32( hb_BLen( cData ) ) + U32( hb_CRC32( cData ) ) + cData

#define FAILS   0       // the message is refused
#define EMPTY   1       // read, and the field is not written again
#define IS_NULL 2       // read, and the field is written again as a null

/* Takes the full message and, key by key, drops it, sends it as a null and
 * sends it with another type: per form, whether the message is still read
 * and what the field then holds, seen by writing again what was read. */
STATIC PROCEDURE TestEveryKeyByKey()
   /* per form: its first and last key, and what happens when the key is dropped, null, of another type */
   LOCAL aForms := { ;
      {  1, 10, { FAILS, FAILS, FAILS } }, ;       // required
      { 11, 20, { EMPTY, EMPTY, EMPTY } }, ;       // optional
      { 21, 30, { FAILS, IS_NULL, FAILS } }, ;     // nullable
      { 31, 40, { EMPTY, EMPTY, EMPTY } }, ;       // optional nullable: it reads NIL, and that null is not written
      { 41, 50, { EMPTY, EMPTY, EMPTY } }, ;       // list
      { 51, 51, { FAILS, FAILS, FAILS } }, ;       // tupleset
      { 52, 52, { EMPTY, EMPTY, EMPTY } } }        // optional tupleset
   LOCAL aOn := { "on_int32", "on_int64", "on_ts", "on_text", "on_bin", "on_bool", "on_float64", "on_ds", "on_msg", "on_rec" }
   LOCAL cFull := hb_HexToStr( EveryFullHex() )
   LOCAL aForm, nKey, nHow, nWant, nErr, o, lOk, cKeys, nBad := 0

   /* the change itself is sound: a key nobody has leaves the message as it was */
   CHECK Change( cFull, 999, DROP_KEY ) == cFull

   FOR EACH aForm IN aForms
      FOR nKey := aForm[ 1 ] TO aForm[ 2 ]
         FOR nHow := DROP_KEY TO OTHER_KEY
            nWant := aForm[ 3 ][ nHow ]
            o := ApiEvery():New()
            lOk := o:Decode( Change( cFull, nKey, nHow ), @nErr )
            IF nWant == FAILS
               IF lOk .OR. nErr != KVP_E_MISSING
                  nBad++
                  ? "key", nKey, "case", nHow, "read, want it refused", nErr
               ENDIF
               LOOP
            ENDIF
            IF ! lOk
               nBad++
               ? "key", nKey, "case", nHow, "refused", nErr
               LOOP
            ENDIF
            cKeys := "," + KeysOf( o:Encode() ) + ","
            IF nWant == EMPTY .AND. ( "," + hb_ntos( nKey ) + "," $ cKeys .OR. ",-" + hb_ntos( nKey ) + "," $ cKeys )
               nBad++
               ? "key", nKey, "case", nHow, "the field holds something"
            ENDIF
            IF nWant == IS_NULL .AND. ! ( ",-" + hb_ntos( nKey ) + "," $ cKeys )
               nBad++
               ? "key", nKey, "case", nHow, "the field is not null"
            ENDIF
            /* the null of an optional nullable field is there to see */
            IF nKey >= 31 .AND. nKey <= 40 .AND. __objSendMsg( o, aOn[ nKey - 30 ] ) != NIL
               nBad++
               ? "key", nKey, "case", nHow, "the field is not NIL"
            ENDIF
         NEXT
      NEXT
   NEXT
   CHECK nBad == 0
   RETURN

/* --- the server side ---------------------------------------------------------
 * The multiplexer generated from api.kvp (ApiServer), with no transport: a
 * handler of the application, the message it gets for each call, and a
 * context the multiplexer only passes on. Here the context is a hash where
 * the handler leaves the bytes of what it replies.
 */
CREATE CLASS TestHandler

   VAR nCalls INIT 0

   METHOD OnLogin( oMsg, oCtx )
   METHOD OnPing( oMsg, oCtx )
   METHOD OnQuery( oMsg, oCtx )
   METHOD OnEvery( oMsg, oCtx )

ENDCLASS

METHOD OnLogin( oMsg, oCtx ) CLASS TestHandler

   ::nCalls++
   oCtx[ "class" ] := oMsg:ClassName()
   IF oMsg:request:password == "secret"
      oMsg:reply:name := Upper( oMsg:request:user )      // the reply is there, empty: just fill it
      oMsg:reply:expires := 1700000000
      oCtx[ "reply" ] := oMsg:Encode()
   ELSE
      oCtx[ "reply" ] := KvpEncode( oMsg:PackError( "wrong password" ) )
   ENDIF

   RETURN NIL

METHOD OnPing( oMsg, oCtx ) CLASS TestHandler

   ::nCalls++
   oCtx[ "reply" ] := iif( oMsg:reply == NIL, oMsg:Encode(), "ping has a reply?" )

   RETURN NIL

METHOD OnQuery( oMsg, oCtx ) CLASS TestHandler

   ::nCalls++
   oMsg:reply:code := Len( oMsg:request:sql )
   oMsg:reply:rows:AddField( "n", KVP_INT32 )            // a new reply has its tupleset ready
   oMsg:reply:rows:AddRow( { 1 } )
   oCtx[ "reply" ] := oMsg:Encode()

   RETURN NIL

METHOD OnEvery( oMsg, oCtx ) CLASS TestHandler

   ::nCalls++
   oCtx[ "reply" ] := oMsg:request:r_text                // nothing says a handler must reply a message

   RETURN NIL

/* A message class of the application's own, put in the place of the generated one. */
CREATE CLASS MyLoginMsg INHERIT ApiLoginMsg
ENDCLASS

STATIC FUNCTION Call( oSrv, oReq, hCtx, nErr )
   RETURN oSrv:Dispatch( KvpParse( oReq:Encode() ), hCtx, @nErr )

STATIC PROCEDURE TestServer()
   LOCAL oH := TestHandler():New(), oSrv := ApiServer():New( oH ), hCtx := { => }
   LOCAL nErr, oReq, oRep, hMsg

   /* a call: the request is unpacked into the message, the handler fills the reply */
   oReq := ApiLogin():New()
   oReq:user     := "bob"
   oReq:password := "secret"
   CHECK Call( oSrv, oReq, hCtx, @nErr ) .AND. nErr == KVP_OK .AND. oH:nCalls == 1
   CHECK hCtx[ "class" ] == "APILOGINMSG"
   oRep := ApiLoginReply():New()
   CHECK oRep:Decode( hCtx[ "reply" ], @nErr ) .AND. oRep:name == "BOB" .AND. oRep:expires == 1700000000
   CHECK Len( oRep:events ) == 0 .AND. oRep:owner == NIL            // what the handler did not touch is empty

   /* an error reply is the handler's to send */
   oReq:password := "no"
   CHECK Call( oSrv, oReq, hCtx, @nErr ) .AND. nErr == KVP_OK
   CHECK ! oRep:Decode( hCtx[ "reply" ], @nErr ) .AND. nErr == KVP_E_REPLY
   CHECK KvpErrorText( KvpParse( hCtx[ "reply" ] ) ) == "wrong password"

   /* a command with no reply: an empty message of its number */
   CHECK Call( oSrv, ApiHeartbeat():New(), hCtx, @nErr ) .AND. nErr == KVP_OK    // the request of ping, as the declaration names it
   hMsg := KvpParse( hCtx[ "reply" ] )
   CHECK hMsg != NIL .AND. KvpCommand( hMsg ) == API_CMD_PING .AND. KvpCount( hMsg ) == 0

   /* a reply with a tupleset */
   oReq := ApiQuery():New()
   oReq:sql := "select"
   CHECK Call( oSrv, oReq, hCtx, @nErr ) .AND. nErr == KVP_OK
   oRep := ApiQueryReply():New()
   CHECK oRep:Decode( hCtx[ "reply" ], @nErr ) .AND. oRep:code == 6 .AND. Len( oRep:rows:aRows ) == 1 .AND. oRep:rows:aRows[ 1 ][ 1 ] == 1

   /* the context is the caller's: the handler may leave anything in it */
   oReq := ApiEvery():New()
   oReq:r_text := "as it came"
   CHECK Call( oSrv, oReq, hCtx, @nErr ) .AND. hCtx[ "reply" ] == "as it came"
   CHECK oH:nCalls == 5

   /* a command that is none of the service: nobody is called */
   CHECK ! oSrv:Dispatch( KvpParse( KvpEncode( KvpNew( 99 ) ) ), hCtx, @nErr ) .AND. nErr == KVP_E_NOT_FOUND
   /* a request with a required field missing: nobody is called either */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, API_LOGIN_USER, "bob" )
   CHECK ! oSrv:Dispatch( KvpParse( KvpEncode( hMsg ) ), hCtx, @nErr ) .AND. nErr == KVP_E_MISSING
   CHECK oH:nCalls == 5

   /* the message of a command is created from the class function in the
      multiplexer: the application may put its own there */
   oSrv:login := @MyLoginMsg()
   oReq := ApiLogin():New()
   oReq:user     := "ann"
   oReq:password := "secret"
   CHECK Call( oSrv, oReq, hCtx, @nErr ) .AND. hCtx[ "class" ] == "MYLOGINMSG"
   oRep := ApiLoginReply():New()
   CHECK oRep:Decode( hCtx[ "reply" ], @nErr ) .AND. oRep:name == "ANN"
   RETURN

/* --- the client side ---------------------------------------------------------
 * The client generated from api.kvp (ApiClient), over a transport that is
 * none: it hands the request straight to the multiplexer, in memory, so the
 * two generated sides run against each other.
 */
CREATE CLASS LoopTransport

   VAR oSrv
   VAR lDown INIT .F.

   METHOD Call( cRequest, nErr )

ENDCLASS

METHOD Call( cRequest, nErr ) CLASS LoopTransport

   LOCAL hCtx := { => }

   IF ::lDown
      RETURN NIL                                       // could not, and says no more
   ENDIF
   IF ! ::oSrv:Dispatch( KvpParse( cRequest ), hCtx, @nErr )
      RETURN NIL
   ENDIF

   RETURN hCtx[ "reply" ]

STATIC PROCEDURE TestClient()
   LOCAL oH := TestHandler():New(), oT := LoopTransport():New(), oCli, oReq, oRep, nErr

   oT:oSrv := ApiServer():New( oH )
   oCli := ApiClient():New( oT )

   /* a call: the request out, the reply back, both as objects */
   oReq := ApiLogin():New()
   oReq:user     := "bob"
   oReq:password := "secret"
   oRep := oCli:Login( oReq, @nErr )
   CHECK oRep != NIL .AND. nErr == KVP_OK .AND. oRep:ClassName() == "APILOGINREPLY"
   CHECK oRep != NIL .AND. oRep:name == "BOB" .AND. oRep:expires == 1700000000

   /* an error reply: no object, and the server's text */
   oReq:password := "no"
   CHECK oCli:Login( oReq, @nErr ) == NIL .AND. nErr == KVP_E_REPLY .AND. oCli:cError == "wrong password"

   /* a command with no reply says .T. or .F. */
   CHECK oCli:Ping( ApiHeartbeat():New(), @nErr ) == .T. .AND. nErr == KVP_OK .AND. oCli:cError == ""

   /* a reply with a tupleset */
   oReq := ApiQuery():New()
   oReq:sql := "select"
   oRep := oCli:Query( oReq, @nErr )
   CHECK oRep != NIL .AND. oRep:code == 6 .AND. Len( oRep:rows:aRows ) == 1

   /* a reply that is no message: the handler of every leaves plain text */
   CHECK oCli:Every( ApiEvery():New(), @nErr ) == .F. .AND. nErr != KVP_OK

   /* the transport could not: its own error, or KVP_E_CALL when it gives none */
   oReq := ApiLogin():New()                            // no password: the multiplexer refuses it
   oReq:password := NIL
   oT:lDown := .T.
   CHECK oCli:Login( oReq, @nErr ) == NIL .AND. nErr == KVP_E_CALL
   CHECK oH:nCalls == 5
   RETURN

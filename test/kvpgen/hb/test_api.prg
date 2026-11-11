/*
 * test_api.prg - exercises the Harbour code kvpack generates from
 * test/kvpgen/api.kvp: the canonical reply shared with the Go and C tests,
 * written and read back, plus missing fields and error replies.
 *   hbmk2 test_api.hbp
 * Exit code 0 when all good; each failure prints its line.
 */
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

PROCEDURE Main()
   TestWriteCanonical()
   TestReadCanonical()
   TestErrors()
   IF s_nFail > 0
      ? s_nFail, "failure(s)"
      ErrorLevel( 1 )
   ELSE
      ? "kvpack generated Harbour: all good"
   ENDIF
   RETURN

STATIC FUNCTION CanonicalReply()
   RETURN { "name" => "ann", "expires" => 1700000000, ;
            "events" => { { "id" => 7, "title" => "a", "codes" => { 1, 2 }, "who" => { "name" => "bo" } }, ;
                          { "id" => 8, "title" => "b", "big" => 5, "who" => { "name" => "cy" }, ;
                            "active" => .T., "score" => 2.5, "day" => hb_SToD( "20261111" ) } }, ;
            "owner" => { "name" => "di" } }

STATIC PROCEDURE TestWriteCanonical()
   CHECK Lower( hb_StrToHex( ApiLoginReplyEncode( CanonicalReply() ) ) ) == CANONICAL_HEX
   RETURN

STATIC PROCEDURE TestReadCanonical()
   LOCAL nErr, hR := ApiLoginReplyDecode( hb_HexToStr( CANONICAL_HEX ), @nErr )

   CHECK hR != NIL .AND. nErr == KVP_OK
   IF hR == NIL
      RETURN
   ENDIF
   CHECK hR[ "name" ] == "ann"
   CHECK hR[ "expires" ] == 1700000000
   CHECK hR[ "level" ] == 0
   CHECK hR[ "owner" ][ "name" ] == "di"
   CHECK Len( hR[ "events" ] ) == 2
   CHECK hR[ "events" ][ 1 ][ "id" ] == 7 .AND. hR[ "events" ][ 1 ][ "title" ] == "a"
   CHECK hR[ "events" ][ 1 ][ "big" ] == 0 .AND. hR[ "events" ][ 1 ][ "blob" ] == "" .AND. hR[ "events" ][ 1 ][ "raw" ] == NIL
   CHECK Len( hR[ "events" ][ 1 ][ "codes" ] ) == 2 .AND. hR[ "events" ][ 1 ][ "codes" ][ 2 ] == 2
   CHECK hR[ "events" ][ 1 ][ "who" ][ "name" ] == "bo"
   CHECK hR[ "events" ][ 2 ][ "id" ] == 8 .AND. hR[ "events" ][ 2 ][ "big" ] == 5
   CHECK hR[ "events" ][ 2 ][ "who" ][ "name" ] == "cy"
   CHECK hR[ "events" ][ 1 ][ "active" ] == .F. .AND. hR[ "events" ][ 1 ][ "score" ] == 0 .AND. Empty( hR[ "events" ][ 1 ][ "day" ] )
   CHECK hR[ "events" ][ 2 ][ "active" ] == .T. .AND. hR[ "events" ][ 2 ][ "score" ] == 2.5
   CHECK DToS( hR[ "events" ][ 2 ][ "day" ] ) == "20261111"
   RETURN

STATIC PROCEDURE TestErrors()
   LOCAL nErr, hMsg

   /* password missing */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, API_LOGIN_USER, "bob" )
   CHECK ApiLoginDecode( KvpEncode( hMsg ), @nErr ) == NIL .AND. nErr == KVP_E_MISSING

   /* password of another type */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, API_LOGIN_USER, "bob" )
   KvpAddInt32( hMsg, API_LOGIN_PASSWORD, 5 )
   CHECK ApiLoginDecode( KvpEncode( hMsg ), @nErr ) == NIL .AND. nErr == KVP_E_MISSING

   /* optional left out: user and password only */
   CHECK Len( ApiLoginEncode( { "user" => "bob", "password" => "x" } ) ) == 20 + 2 * 8 + 4

   /* error reply */
   hMsg := KvpNew( API_CMD_LOGIN )
   KvpAddText( hMsg, KVP_KEY_ERROR, "wrong password" )
   CHECK ApiLoginReplyDecode( KvpEncode( hMsg ), @nErr ) == NIL .AND. nErr == KVP_E_REPLY

   /* not a message */
   CHECK ApiLoginDecode( "hello", @nErr ) == NIL .AND. nErr == KVP_E_TRUNCATED
   RETURN

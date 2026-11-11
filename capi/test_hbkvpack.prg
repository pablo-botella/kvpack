/*
 * test_hbkvpack.prg — exercises the Harbour wrapper: the canonical message
 * shared with the Go and C tests (same bytes), lists, nested messages,
 * error replies, rejections and handles left to the garbage collector.
 *   hbmk2 test_hbkvpack.hbp
 * Exit code 0 when all good; each failure prints its line.
 */
#include "kvpack.ch"

STATIC s_nFail := 0

#xcommand CHECK <expr> => IF !( <expr> ) ; s_nFail++ ; ? "FAIL line", ProcLine(), <"expr"> ; ENDIF

// command 7, int32 -2, int64 1234567890123, ts 1700000000, text "héllo", bin 00 ff 10, keys 1..5
#define CANONICAL_HEX "4b565001070000000500000045000000a12331480100010004000000feffffff0200020008000000cb04fb711f010000030003000800000000f1536500000000040004000600000068c3a96c6c6f050005000300000000ff10"

// command 8, bool true, bool false, float64 1.5, ds "20261111", null ds (8 spaces), keys 1..5
#define CANONICAL2_HEX "4b5650010800000005000000420000009000fc760100070001000000010200070001000000000300080008000000000000000000f83f0400090008000000323032363131313105000900080000002020202020202020"

PROCEDURE Main()
   TestWriteCanonical()
   TestReadCanonical()
   TestCanonical2()
   TestListsAndNested()
   TestErrorsAndRejects()
   TestGarbage()
   IF s_nFail > 0
      ? s_nFail, "failure(s)"
      ErrorLevel( 1 )
   ELSE
      ? "kvpack Harbour: all good"
   ENDIF
   RETURN

STATIC PROCEDURE TestCanonical2()
   LOCAL hMsg := KvpNew( 8 ), hIn, nErr

   KvpAddBool( hMsg, 1, .T. )
   KvpAddBool( hMsg, 2, .F. )
   KvpAddFloat64( hMsg, 3, 1.5 )
   KvpAddDs( hMsg, 4, hb_SToD( "20261111" ) )
   KvpAddDs( hMsg, 5, hb_SToD( "" ) )
   CHECK Lower( hb_StrToHex( KvpEncode( hMsg ) ) ) == CANONICAL2_HEX

   hIn := KvpParse( hb_HexToStr( CANONICAL2_HEX ), @nErr )
   CHECK hIn != NIL .AND. nErr == KVP_OK
   CHECK KvpGet( hIn, 1 ) == .T.
   CHECK KvpGet( hIn, 2 ) == .F.
   CHECK KvpGet( hIn, 3 ) == 1.5
   CHECK DToS( KvpGet( hIn, 4 ) ) == "20261111"
   CHECK Empty( KvpGet( hIn, 5 ) ) .AND. ValType( KvpGet( hIn, 5 ) ) == "D"
   CHECK KvpType( hIn, 4 ) == KVP_DS
   RETURN

STATIC PROCEDURE TestWriteCanonical()
   LOCAL hMsg := KvpNew( 7 ), cBytes

   KvpAddInt32( hMsg, 1, -2 )
   KvpAddInt64( hMsg, 2, 1234567890123 )
   KvpAddTs( hMsg, 3, 1700000000 )
   KvpAddText( hMsg, 4, "h" + Chr( 0xC3 ) + Chr( 0xA9 ) + "llo" )
   KvpAddBin( hMsg, 5, Chr( 0 ) + Chr( 0xFF ) + Chr( 0x10 ) )
   cBytes := KvpEncode( hMsg )
   CHECK Len( cBytes ) == 89
   CHECK Lower( hb_StrToHex( cBytes ) ) == CANONICAL_HEX
   // the writer is still usable after encoding
   KvpAddInt32( hMsg, 6, 1 )
   CHECK Len( KvpEncode( hMsg ) ) == 89 + 12
   RETURN

STATIC PROCEDURE TestReadCanonical()
   LOCAL hMsg, nErr := 99, aItem, i

   hMsg := KvpParse( hb_HexToStr( CANONICAL_HEX ), @nErr )
   CHECK hMsg != NIL .AND. nErr == KVP_OK
   CHECK KvpCommand( hMsg ) == 7
   CHECK KvpCount( hMsg ) == 5
   CHECK KvpGet( hMsg, 1 ) == -2
   CHECK KvpGet( hMsg, 2 ) == 1234567890123
   CHECK KvpGet( hMsg, 3 ) == 1700000000
   CHECK KvpGet( hMsg, 4 ) == "h" + Chr( 0xC3 ) + Chr( 0xA9 ) + "llo"
   CHECK KvpGet( hMsg, 5 ) == Chr( 0 ) + Chr( 0xFF ) + Chr( 0x10 )
   CHECK KvpGet( hMsg, 9 ) == NIL
   CHECK KvpGet( hMsg, 1, 2 ) == NIL          // no second item under key 1
   CHECK KvpType( hMsg, 3 ) == KVP_TS
   CHECK KvpType( hMsg, 9 ) == 0
   CHECK KvpErrorText( hMsg ) == NIL
   FOR i := 1 TO 5
      aItem := KvpItem( hMsg, i )
      CHECK aItem[ KVP_ITEM_KEY ] == i .AND. aItem[ KVP_ITEM_TYPE ] == i
   NEXT
   CHECK KvpItem( hMsg, 6 ) == NIL
   CHECK KvpItem( hMsg, 0 ) == NIL
   CHECK KvpTypeStr( KVP_MSG ) == "msg"
   RETURN

STATIC FUNCTION MakeRow( nId, cName )
   LOCAL h := KvpNew( 100 )
   KvpAddInt32( h, 1, nId )
   KvpAddText( h, 2, cName )
   RETURN h

STATIC PROCEDURE TestListsAndNested()
   LOCAL hMsg := KvpNew( 5 ), hIn, hSub, aRows, i, cBytes

   KvpAddMsg( hMsg, 1, MakeRow( 1, "a" ) )
   KvpAddMsg( hMsg, 1, MakeRow( 2, "b" ) )
   KvpAddText( hMsg, 2, "x" )
   KvpAddText( hMsg, 2, "y" )
   cBytes := KvpEncode( hMsg )
   hIn := KvpParse( cBytes )
   CHECK hIn != NIL
   CHECK KvpCount( hIn ) == 4
   aRows := KvpGetAll( hIn, 1 )
   CHECK Len( aRows ) == 2
   FOR i := 1 TO Len( aRows )
      CHECK KvpCommand( aRows[ i ] ) == 100
      CHECK KvpGet( aRows[ i ], 1 ) == i
   NEXT
   hSub := KvpGet( hIn, 1, 2 )
   CHECK hSub != NIL .AND. KvpGet( hSub, 2 ) == "b"
   CHECK KvpGet( hIn, 2 ) == "x" .AND. KvpGet( hIn, 2, 2 ) == "y"
   CHECK Len( KvpGetAll( hIn, 2 ) ) == 2
   CHECK Len( KvpGetAll( hIn, 9 ) ) == 0
   // a parsed message can be nested again, as bytes
   hMsg := KvpNew( 6 )
   KvpAddMsg( hMsg, 1, hIn )
   hIn := KvpParse( KvpEncode( hMsg ) )
   CHECK hIn != NIL .AND. KvpCommand( KvpGet( hIn, 1 ) ) == 5
   // the child survives its parent
   hIn := NIL
   hb_gcAll()
   CHECK KvpGet( hSub, 1 ) == 2
   RETURN

STATIC PROCEDURE TestErrorsAndRejects()
   LOCAL hMsg, nErr, cGood := hb_HexToStr( CANONICAL_HEX ), cBad

   hMsg := KvpNew( 9 )
   KvpAddText( hMsg, KVP_KEY_ERROR, "no such user" )
   hMsg := KvpParse( KvpEncode( hMsg ) )
   CHECK KvpErrorText( hMsg ) == "no such user"
   CHECK KvpCommand( hMsg ) == 9

   // empty message
   hMsg := KvpParse( KvpEncode( KvpNew( 3 ) ), @nErr )
   CHECK hMsg != NIL .AND. KvpCount( hMsg ) == 0 .AND. KvpCommand( hMsg ) == 3

   cBad := "X" + SubStr( cGood, 2 )
   CHECK KvpParse( cBad, @nErr ) == NIL .AND. nErr == KVP_E_MAGIC
   CHECK KvpParse( Left( cGood, 10 ), @nErr ) == NIL .AND. nErr == KVP_E_TRUNCATED
   CHECK KvpParse( cGood + "x", @nErr ) == NIL .AND. nErr == KVP_E_FORMAT
   cBad := Left( cGood, Len( cGood ) - 1 ) + Chr( 0x11 )
   CHECK KvpParse( cBad, @nErr ) == NIL .AND. nErr == KVP_E_CRC
   CHECK KvpParse( "", @nErr ) == NIL .AND. nErr == KVP_E_TRUNCATED
   CHECK KvpErrorStr( KVP_E_CRC ) == "crc32 mismatch"

   // a closed-over wrong handle kind raises a catchable runtime error
   hMsg := KvpNew( 1 )
   BEGIN SEQUENCE WITH {| oErr | Break( oErr ) }
      KvpCount( hMsg )
      CHECK .F.
   RECOVER
      CHECK .T.
   END SEQUENCE
   RETURN

STATIC PROCEDURE TestGarbage()
   LOCAL i, h, hIn

   FOR i := 1 TO 5000
      h := KvpNew( i )
      KvpAddText( h, 1, Replicate( "x", 100 ) )
      hIn := KvpParse( KvpEncode( h ) )
      KvpGet( hIn, 1 )
   NEXT
   h := NIL
   hIn := NIL
   hb_gcAll()
   CHECK .T.
   RETURN

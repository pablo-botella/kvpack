/*
 * test_hbkvpack.prg — exercises the Harbour wrapper: the canonical message
 * shared with the Go and C tests (same bytes), lists, nested messages,
 * error replies, rejections and handles left to the garbage collector.
 *   hbmk2 test_hbkvpack.hbp
 * Exit code 0 when all good; each failure prints its line.
 */
#include "hbclass.ch"
#include "kvpack.ch"

STATIC s_nFail := 0

#xcommand CHECK <expr> => IF !( <expr> ) ; s_nFail++ ; ? "FAIL line", ProcLine(), <"expr"> ; ENDIF

// command 7, int32 -2, int64 1234567890123, ts 1700000000, text "héllo", bin 00 ff 10, keys 1..5
#define CANONICAL_HEX "4b565001070000000500000045000000a12331480100010004000000feffffff0200020008000000cb04fb711f010000030003000800000000f1536500000000040004000600000068c3a96c6c6f050005000300000000ff10"

// command 8, bool true, bool false, float64 1.5, ds "20261111", null ds (8 spaces), keys 1..5
#define CANONICAL2_HEX "4b5650010800000005000000420000009000fc760100070001000000010200070001000000000300080008000000000000000000f83f0400090008000000323032363131313105000900080000002020202020202020"

// command 9, int32 7, null, an empty text, keys 1..3
#define CANONICAL3_HEX "4b56500109000000030000001c000000a2ae13ae01000100040000000700000002000a00000000000300040000000000"

// command 10: under key 1 a definition (id int32, name text with meta "len=40", born ds) and the
// tuples 1, "Ana", 1980-01-15 and 2, "Luis", null; under key 2 a definition (n int64) with no tuples
#define CANONICAL4_HEX ;
   "4b5650010a000000040000009c000000f158c35d01000b0032000000030000000100020000006964000000000400040000" + ;
   "006e616d65060000006c656e3d3430090004000000626f726e0000000001000c0021000000010004000000010000000400" + ;
   "03000000416e61090008000000313938303031313501000c001a000000010004000000020000000400040000004c756973" + ;
   "0a000000000002000b000f000000010000000200010000006e00000000"

PROCEDURE Main()
   TestWriteCanonical()
   TestReadCanonical()
   TestCanonical2()
   TestCanonical3()
   TestCanonical4()
   TestTupleSetClass()
   TestTupleOrder()
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

STATIC PROCEDURE TestCanonical3()
   LOCAL hMsg := KvpNew( 9 ), hIn, nErr, aItem, aAll

   KvpAddInt32( hMsg, 1, 7 )
   KvpAddNull( hMsg, 2 )
   KvpAddText( hMsg, 3, "" )
   CHECK Lower( hb_StrToHex( KvpEncode( hMsg ) ) ) == CANONICAL3_HEX

   hIn := KvpParse( hb_HexToStr( CANONICAL3_HEX ), @nErr )
   CHECK hIn != NIL .AND. nErr == KVP_OK
   CHECK KvpIsNull( hIn, 2 )
   CHECK ! KvpIsNull( hIn, 1 ) .AND. ! KvpIsNull( hIn, 3 )
   CHECK ! KvpIsNull( hIn, 9 )                 // absent is not null
   CHECK KvpGet( hIn, 2 ) == NIL
   CHECK KvpType( hIn, 2 ) == KVP_NULL .AND. KvpType( hIn, 9 ) == 0
   CHECK KvpGet( hIn, 3 ) == ""
   aItem := KvpItem( hIn, 2 )
   CHECK aItem[ KVP_ITEM_KEY ] == 2 .AND. aItem[ KVP_ITEM_TYPE ] == KVP_NULL .AND. aItem[ KVP_ITEM_VALUE ] == NIL
   aAll := KvpGetAll( hIn, 2 )
   CHECK Len( aAll ) == 1 .AND. aAll[ 1 ] == NIL
   CHECK KvpTypeStr( KVP_NULL ) == "null"
   RETURN

STATIC PROCEDURE TestCanonical4()
   LOCAL hMsg := KvpNew( 10 ), hIn, nErr, aDef, aRows, aAll

   KvpAddTupleDef( hMsg, 1, { { "id", KVP_INT32 }, { "name", KVP_TEXT, "len=40" }, { "born", KVP_DS } } )
   KvpAddTuple( hMsg, 1, { 1, "Ana", hb_SToD( "19800115" ) } )
   KvpAddTuple( hMsg, 1, { 2, "Luis", NIL } )
   KvpAddTupleDef( hMsg, 2, { { "n", KVP_INT64 } } )
   CHECK Lower( hb_StrToHex( KvpEncode( hMsg ) ) ) == CANONICAL4_HEX

   hIn := KvpParse( hb_HexToStr( CANONICAL4_HEX ), @nErr )
   CHECK hIn != NIL .AND. nErr == KVP_OK
   IF hIn == NIL
      RETURN
   ENDIF
   aDef := KvpTupleDef( hIn, 1 )
   CHECK Len( aDef ) == 3
   CHECK aDef[ 1 ][ KVP_FIELD_NAME ] == "id" .AND. aDef[ 1 ][ KVP_FIELD_TYPE ] == KVP_INT32 .AND. aDef[ 1 ][ KVP_FIELD_META ] == ""
   CHECK aDef[ 2 ][ KVP_FIELD_NAME ] == "name" .AND. aDef[ 2 ][ KVP_FIELD_TYPE ] == KVP_TEXT .AND. aDef[ 2 ][ KVP_FIELD_META ] == "len=40"
   CHECK aDef[ 3 ][ KVP_FIELD_NAME ] == "born" .AND. aDef[ 3 ][ KVP_FIELD_TYPE ] == KVP_DS

   aRows := KvpTuples( hIn, 1 )
   CHECK Len( aRows ) == 2
   CHECK aRows[ 1 ][ 1 ] == 1 .AND. aRows[ 1 ][ 2 ] == "Ana" .AND. DToS( aRows[ 1 ][ 3 ] ) == "19800115"
   CHECK aRows[ 2 ][ 1 ] == 2 .AND. aRows[ 2 ][ 2 ] == "Luis" .AND. aRows[ 2 ][ 3 ] == NIL

   // the definition with no tuples, and a key with no definition
   CHECK Len( KvpTupleDef( hIn, 2 ) ) == 1 .AND. Len( KvpTuples( hIn, 2 ) ) == 0
   CHECK KvpTupleDef( hIn, 9 ) == NIL .AND. KvpTuples( hIn, 9 ) == NIL

   // the generic readers see the definition and then its tuples
   CHECK KvpType( hIn, 1 ) == KVP_TUPLEDEF
   CHECK Len( KvpGet( hIn, 1 ) ) == 3 .AND. KvpGet( hIn, 1 )[ 2 ][ KVP_FIELD_NAME ] == "name"
   CHECK KvpGet( hIn, 1, 3 )[ 2 ] == "Luis"
   aAll := KvpGetAll( hIn, 1 )
   CHECK Len( aAll ) == 3 .AND. aAll[ 2 ][ 2 ] == "Ana"
   CHECK KvpItem( hIn, 2 )[ KVP_ITEM_TYPE ] == KVP_TUPLE .AND. KvpItem( hIn, 2 )[ KVP_ITEM_VALUE ][ 1 ] == 1
   CHECK KvpTypeStr( KVP_TUPLEDEF ) == "tupledef" .AND. KvpTypeStr( KVP_TUPLE ) == "tuple"
   RETURN

/* KvpTupleSet (kvpclass.prg): the same canonical message through the class,
 * what it refuses, and KvpStruct over a class written by hand. */
STATIC PROCEDURE TestTupleSetClass()
   LOCAL hMsg := KvpNew( 10 ), hIn, nErr
   LOCAL oSet := KvpTupleSet():New(), oEmpty := KvpTupleSet():New(), oIn := KvpTupleSet():New()
   LOCAL oAll, oBox

   CHECK oSet:AddField( "id", KVP_INT32 )
   CHECK oSet:AddField( "name", KVP_TEXT, "len=40" )
   CHECK oSet:AddField( "born", KVP_DS )
   CHECK oSet:AddRow( { 1, "Ana", hb_SToD( "19800115" ) } )
   CHECK oSet:AddRow( { 2, "Luis", NIL } )
   CHECK oEmpty:AddField( "n", KVP_INT64 )
   oSet:AddTo( hMsg, 1 )
   oEmpty:AddTo( hMsg, 2 )
   CHECK Lower( hb_StrToHex( KvpEncode( hMsg ) ) ) == CANONICAL4_HEX

   // the fields, and the tuples as an array of arrays
   CHECK Len( oSet:aFields ) == 3 .AND. Len( oSet:aRows ) == 2 .AND. Len( oEmpty:aRows ) == 0
   CHECK oSet:FieldPos( "name" ) == 2 .AND. oSet:FieldPos( "NAME" ) == 0 .AND. oSet:FieldPos( "x" ) == 0
   CHECK oSet:aRows[ 2 ][ oSet:FieldPos( "name" ) ] == "Luis" .AND. oSet:aRows[ 2 ][ 3 ] == NIL
   CHECK oSet:aFields[ 2 ][ KVP_FIELD_META ] == "len=40" .AND. oSet:aFields[ 1 ][ KVP_FIELD_META ] == ""

   // what a reader would refuse is not added
   CHECK ! oSet:AddField( "late", KVP_INT32 )                    // there are tuples already
   CHECK ! oEmpty:AddField( "n", KVP_TEXT )                      // a repeated name
   CHECK ! oEmpty:AddField( "", KVP_TEXT ) .AND. ! oEmpty:AddField( 5, KVP_TEXT )
   CHECK ! oEmpty:AddField( "t", KVP_TUPLE ) .AND. ! oEmpty:AddField( "z", KVP_NULL ) .AND. ! oEmpty:AddField( "q" )
   CHECK Len( oEmpty:aFields ) == 1
   CHECK ! oSet:AddRow( { 1, "x" } )                             // one value short
   CHECK ! oSet:AddRow( { 1, "x", NIL, NIL } )                   // one too many
   CHECK ! oSet:AddRow( { "1", "x", NIL } )                      // a text for the int32
   CHECK ! oSet:AddRow( { 1, 2, NIL } )                          // a number for the text
   CHECK ! oSet:AddRow( { 1, "x", "19800115" } )                 // a text for the ds
   CHECK ! oSet:AddRow( { 5000000000, "x", NIL } )               // too big for an int32
   CHECK ! oSet:AddRow( "row" ) .AND. ! oIn:AddRow( {} )         // not an array; no definition
   CHECK Len( oSet:aRows ) == 2
   CHECK oSet:Fits( { NIL, NIL, NIL } ) .AND. ! oSet:Fits( { NIL, NIL } )

   // reading
   hIn := KvpParse( hb_HexToStr( CANONICAL4_HEX ), @nErr )
   CHECK hIn != NIL
   IF hIn != NIL
      CHECK oIn:Read( hIn, 1 ) .AND. Len( oIn:aFields ) == 3 .AND. Len( oIn:aRows ) == 2
      CHECK oIn:aFields[ 2 ][ KVP_FIELD_NAME ] == "name" .AND. oIn:aRows[ 1 ][ 2 ] == "Ana" .AND. oIn:aRows[ 2 ][ 3 ] == NIL
      CHECK oIn:Read( hIn, 2 ) .AND. Len( oIn:aFields ) == 1 .AND. Len( oIn:aRows ) == 0
      CHECK ! oIn:Read( hIn, 9 ) .AND. Len( oIn:aFields ) == 1    // no definition: left as it was
   ENDIF

   // every field type, and rows assigned as they are
   oAll := KvpTupleSet():New()
   oAll:AddField( "a", KVP_INT64 )
   oAll:AddField( "b", KVP_TS )
   oAll:AddField( "c", KVP_BIN )
   oAll:AddField( "d", KVP_MSG )
   oAll:AddField( "e", KVP_BOOL )
   oAll:AddField( "f", KVP_FLOAT64 )
   CHECK oAll:AddRow( { 1234567890123, 1700000000, Chr( 0 ) + Chr( 255 ), MakeRow( 3, "x" ), .T., 1.5 } )
   CHECK ! oAll:AddRow( { 1, 2, "", "not a message", .T., 1.5 } ) .AND. ! oAll:AddRow( { 1, 2, "", NIL, 1, 1.5 } )
   oAll:aRows := { { 1, 2, "x", NIL, .F., 0.5 }, { NIL, NIL, NIL, NIL, NIL, NIL } }
   hMsg := KvpNew( 1 )
   oAll:AddTo( hMsg, 7 )
   hIn := KvpParse( KvpEncode( hMsg ) )
   CHECK hIn != NIL .AND. oIn:Read( hIn, 7 ) .AND. Len( oIn:aRows ) == 2 .AND. oIn:aRows[ 1 ][ 3 ] == "x" .AND. oIn:aRows[ 2 ][ 5 ] == NIL
   oAll:aRows := { { "wrong", 2, "x", NIL, .F., 0.5 } }          // what does not fit fails when written
   hMsg := KvpNew( 1 )
   CHECK Raises( {|| oAll:AddTo( hMsg, 7 ) } )

   // KvpStruct: Encode and Decode over the Pack and Unpack of a class of one's own
   oBox := TestBox():New()
   oBox:n := 42
   CHECK oBox:Decode( oBox:Encode(), @nErr ) .AND. nErr == KVP_OK .AND. oBox:n == 42
   CHECK ! oBox:Decode( "junk", @nErr ) .AND. nErr == KVP_E_TRUNCATED
   hMsg := KvpNew( 77 )
   CHECK ! oBox:Decode( KvpEncode( hMsg ), @nErr ) .AND. nErr == KVP_E_MISSING
   RETURN

CREATE CLASS TestBox INHERIT KvpStruct
   VAR n INIT 0
   METHOD Pack()
   METHOD Unpack( hMsg, nErr )
ENDCLASS

METHOD Pack() CLASS TestBox
   LOCAL hMsg := KvpNew( 77 )
   KvpAddInt32( hMsg, 1, ::n )
   RETURN hMsg

METHOD Unpack( hMsg, nErr ) CLASS TestBox
   nErr := KVP_OK
   IF KvpType( hMsg, 1 ) != KVP_INT32
      nErr := KVP_E_MISSING
      RETURN .F.
   ENDIF
   ::n := KvpGet( hMsg, 1 )
   RETURN .T.

STATIC FUNCTION Raises( bCode )
   LOCAL lRaised := .F.

   BEGIN SEQUENCE WITH {| oErr | Break( oErr ) }
      Eval( bCode )
   RECOVER
      lRaised := .T.
   END SEQUENCE
   RETURN lRaised

STATIC PROCEDURE TestTupleOrder()
   LOCAL hMsg, hIn, aRows, aDef := { { "n", KVP_INT32 }, { "s", KVP_TEXT } }

   // every field type, and nulls
   hMsg := KvpNew( 1 )
   KvpAddTupleDef( hMsg, 7, { { "a", KVP_INT32 }, { "b", KVP_INT64 }, { "c", KVP_TS }, { "d", KVP_TEXT }, { "e", KVP_BIN }, ;
                              { "f", KVP_MSG }, { "g", KVP_BOOL }, { "h", KVP_FLOAT64 }, { "i", KVP_DS } } )
   KvpAddTuple( hMsg, 7, { -2, 1234567890123, 1700000000, "", Chr( 0 ) + Chr( 255 ), MakeRow( 3, "x" ), .T., 1.5, hb_SToD( "20261111" ) } )
   KvpAddTuple( hMsg, 7, { NIL, NIL, NIL, NIL, NIL, NIL, NIL, NIL, NIL } )
   hIn := KvpParse( KvpEncode( hMsg ) )
   CHECK hIn != NIL
   aRows := KvpTuples( hIn, 7 )
   CHECK Len( aRows ) == 2
   CHECK aRows[ 1 ][ 1 ] == -2 .AND. aRows[ 1 ][ 2 ] == 1234567890123 .AND. aRows[ 1 ][ 3 ] == 1700000000
   CHECK aRows[ 1 ][ 4 ] == "" .AND. aRows[ 1 ][ 5 ] == Chr( 0 ) + Chr( 255 )
   CHECK KvpCommand( aRows[ 1 ][ 6 ] ) == 100 .AND. KvpGet( aRows[ 1 ][ 6 ], 1 ) == 3
   CHECK aRows[ 1 ][ 7 ] == .T. .AND. aRows[ 1 ][ 8 ] == 1.5 .AND. DToS( aRows[ 1 ][ 9 ] ) == "20261111"
   CHECK AScan( aRows[ 2 ], {| x | x != NIL } ) == 0

   // the writer holds tuples to their order
   hMsg := KvpNew( 1 )
   CHECK Raises( {|| KvpAddTuple( hMsg, 1, { 1, "a" } ) } )                 // no definition
   KvpAddTupleDef( hMsg, 1, aDef )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, aDef ) } )                    // a second definition
   CHECK Raises( {|| KvpAddTuple( hMsg, 2, { 1, "a" } ) } )                 // another key
   CHECK Raises( {|| KvpAddTuple( hMsg, 1, { 1 } ) } )                      // one value short
   CHECK Raises( {|| KvpAddTuple( hMsg, 1, { 1, "a", 2 } ) } )              // one too many
   CHECK Raises( {|| KvpAddTuple( hMsg, 1, { "x", "a" } ) } )               // a text for the int32
   KvpAddTuple( hMsg, 1, { 1, "a" } )
   KvpAddTuple( hMsg, 1, { NIL, NIL } )
   KvpAddInt32( hMsg, 5, 1 )
   CHECK Raises( {|| KvpAddTuple( hMsg, 1, { 1, "a" } ) } )                 // an item in between
   hIn := KvpParse( KvpEncode( hMsg ) )
   CHECK hIn != NIL .AND. Len( KvpTuples( hIn, 1 ) ) == 2 .AND. KvpGet( hIn, 5 ) == 1

   // and a definition a reader would refuse cannot be written
   hMsg := KvpNew( 1 )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, { { "", KVP_INT32 } } ) } )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, { { "a", KVP_INT32 }, { "a", KVP_TEXT } } ) } )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, { { "a", KVP_NULL } } ) } )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, { { "a", KVP_TUPLE } } ) } )
   CHECK Raises( {|| KvpAddTupleDef( hMsg, 1, { "a" } ) } )
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

   // the header alone says how long the message is
   CHECK KvpSize( Left( cGood, 20 ), @nErr ) == Len( cGood ) .AND. nErr == KVP_OK
   CHECK KvpSize( cGood ) == 89
   CHECK KvpSize( KvpEncode( KvpNew( 3 ) ) ) == 20
   CHECK KvpSize( Left( cGood, 19 ), @nErr ) == NIL .AND. nErr == KVP_E_TRUNCATED
   CHECK KvpSize( "X" + SubStr( cGood, 2, 19 ), @nErr ) == NIL .AND. nErr == KVP_E_MAGIC

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

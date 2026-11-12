/*
 * kvpclass.prg — the Harbour classes of kvpack, over the functions of
 * hbkvpack.c:
 *
 *   KvpStruct     the base of the classes kvpack generates from a .kvp: one
 *                 per command, reply and record, with a variable per field
 *   KvpTupleSet   a tuple definition and its tuples: data whose shape is only
 *                 known when the message is built
 */
#include "hbclass.ch"
#include "kvpack.ch"

/* KvpStruct: a generated class defines its variables and the two methods
 * that know its fields, Pack and Unpack; the rest is here.
 *
 *   oReq := ApiLogin():New()
 *   oReq:user := "bob"
 *   cBytes := oReq:Encode()
 *
 *   oReply := ApiLoginReply():New()
 *   IF oReply:Decode( cBytes, @nErr )
 *      ? oReply:name
 *   ENDIF
 */
CREATE CLASS KvpStruct

   METHOD Pack() VIRTUAL             // --> a writer handle with the fields
   METHOD Unpack( hMsg, nErr ) VIRTUAL   // --> .T., or .F. with nErr set
   METHOD Encode()                   // --> the message bytes
   METHOD Decode( cBytes, nErr )     // --> .T., or .F. with nErr set

ENDCLASS

METHOD Encode() CLASS KvpStruct
   RETURN KvpEncode( ::Pack() )

/* Decode fills the object from the bytes of a message. .F. with nErr set
 * when they are no valid message (the error of KvpParse), a required field
 * is missing (KVP_E_MISSING) or the message is an error reply
 * (KVP_E_REPLY): the object is then not to be used. */
METHOD Decode( cBytes, nErr ) CLASS KvpStruct

   LOCAL hMsg := KvpParse( cBytes, @nErr )

   IF hMsg == NIL
      RETURN .F.
   ENDIF

   RETURN ::Unpack( hMsg, @nErr )

/* KvpTupleSet: the fields and the tuples.
 *
 *   aFields   { { cName, nType, cMeta }, ... }, nType a KVP_INT32..KVP_DS;
 *             the meta is the application's own bytes, "" for none
 *   aRows     the tuples, an array of arrays: aRows[ nTuple ][ nField ], of
 *             the Harbour type of the field's type, NIL for a null
 *
 *   oSet := KvpTupleSet():New()
 *   oSet:AddField( "id", KVP_INT32 )
 *   oSet:AddField( "name", KVP_TEXT, "len=40" )
 *   oSet:AddRow( { 1, "Ana" } )
 *   oSet:AddRow( { 2, NIL } )
 *   ? oSet:aRows[ 2 ][ oSet:FieldPos( "name" ) ]
 *
 * AddField and AddRow check what they are given and say .F., adding
 * nothing, when a reader would refuse it. Both arrays may also be assigned
 * as they are (rows that come ready from elsewhere); what does not fit then
 * fails when the message is written.
 */
CREATE CLASS KvpTupleSet

   VAR aFields INIT {}
   VAR aRows   INIT {}

   METHOD AddField( cName, nType, cMeta )   // --> .T., or .F. when refused
   METHOD AddRow( aValues )                 // --> .T., or .F. when refused
   METHOD FieldPos( cName )                 // --> the position of a field, 0 when there is none
   METHOD Fits( aValues )                   // --> .T. when the values are a tuple of the definition
   METHOD AddTo( hMsg, nKey )               // writes the definition and the tuples under nKey
   METHOD Read( hMsg, nKey )                // --> .T., or .F. when nKey holds no definition

ENDCLASS

/* AddField appends a field to the definition. Refused: a name that is
 * empty or already there, a type that is none of KVP_INT32..KVP_DS, and any
 * field once there are tuples. */
METHOD AddField( cName, nType, cMeta ) CLASS KvpTupleSet

   IF ! HB_ISSTRING( cName ) .OR. Len( cName ) == 0 .OR. ! HB_ISNUMERIC( nType ) .OR. nType < KVP_INT32 .OR. nType > KVP_DS
      RETURN .F.
   ENDIF
   IF ::FieldPos( cName ) > 0 .OR. Len( ::aRows ) > 0
      RETURN .F.
   ENDIF
   AAdd( ::aFields, { cName, nType, iif( HB_ISSTRING( cMeta ), cMeta, "" ) } )

   RETURN .T.

METHOD FieldPos( cName ) CLASS KvpTupleSet
   RETURN AScan( ::aFields, {| a | a[ KVP_FIELD_NAME ] == cName } )

/* Fits says whether aValues is a tuple of the definition: one value per
 * field, each NIL or of the Harbour type of its field. */
METHOD Fits( aValues ) CLASS KvpTupleSet

   LOCAL nI, xV, lOk

   IF ! HB_ISARRAY( aValues ) .OR. Len( ::aFields ) == 0 .OR. Len( aValues ) != Len( ::aFields )
      RETURN .F.
   ENDIF
   FOR nI := 1 TO Len( aValues )
      xV := aValues[ nI ]
      IF xV == NIL
         LOOP
      ENDIF
      SWITCH ::aFields[ nI ][ KVP_FIELD_TYPE ]
      CASE KVP_INT32
         lOk := HB_ISNUMERIC( xV ) .AND. xV >= -2147483648 .AND. xV <= 2147483647
         EXIT
      CASE KVP_INT64
      CASE KVP_TS
      CASE KVP_FLOAT64
         lOk := HB_ISNUMERIC( xV )
         EXIT
      CASE KVP_TEXT
      CASE KVP_BIN
         lOk := HB_ISSTRING( xV )
         EXIT
      CASE KVP_BOOL
         lOk := HB_ISLOGICAL( xV )
         EXIT
      CASE KVP_DS
         lOk := HB_ISDATETIME( xV )
         EXIT
      CASE KVP_MSG
         lOk := HB_ISPOINTER( xV )
         EXIT
      OTHERWISE
         lOk := .F.
      ENDSWITCH
      IF ! lOk
         RETURN .F.
      ENDIF
   NEXT

   RETURN .T.

METHOD AddRow( aValues ) CLASS KvpTupleSet

   IF ! ::Fits( aValues )
      RETURN .F.
   ENDIF
   AAdd( ::aRows, aValues )

   RETURN .T.

METHOD AddTo( hMsg, nKey ) CLASS KvpTupleSet

   LOCAL aRow

   KvpAddTupleDef( hMsg, nKey, ::aFields )
   FOR EACH aRow IN ::aRows
      KvpAddTuple( hMsg, nKey, aRow )
   NEXT

   RETURN Self

METHOD Read( hMsg, nKey ) CLASS KvpTupleSet

   LOCAL aDef := KvpTupleDef( hMsg, nKey )

   IF aDef == NIL
      RETURN .F.
   ENDIF
   ::aFields := aDef
   ::aRows := KvpTuples( hMsg, nKey )

   RETURN .T.

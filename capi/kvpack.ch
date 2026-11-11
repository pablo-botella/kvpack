/*
 * kvpack.ch — constants for the Harbour wrapper (hbkvpack.c).
 */
#ifndef KVPACK_CH
#define KVPACK_CH

/* Reserved key of an error reply (a text). */
#define KVP_KEY_ERROR    0

/* Item value types (KvpType, KvpItem). */
#define KVP_INT32        1
#define KVP_INT64        2
#define KVP_TS           3
#define KVP_TEXT         4
#define KVP_BIN          5
#define KVP_MSG          6
#define KVP_BOOL         7
#define KVP_FLOAT64      8
#define KVP_DS           9

/* Positions in the array returned by KvpItem. */
#define KVP_ITEM_KEY     1
#define KVP_ITEM_TYPE    2
#define KVP_ITEM_VALUE   3

/* Error codes (KvpParse's nError, KvpErrorStr). */
#define KVP_OK           0
#define KVP_E_ARGUMENT  -1
#define KVP_E_MAGIC     -2
#define KVP_E_VERSION   -3
#define KVP_E_TRUNCATED -4
#define KVP_E_FORMAT    -5
#define KVP_E_CRC       -6
#define KVP_E_FULL      -7
#define KVP_E_NOT_FOUND -8
#define KVP_E_TYPE      -9
#define KVP_E_MISSING   -10   /* generated code: a required field is missing */
#define KVP_E_REPLY     -11   /* generated code: the reply is an error reply */

#endif /* KVPACK_CH */

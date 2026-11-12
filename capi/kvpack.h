/*
 * kvpack.h — writer and reader for kvpack messages in plain C.
 *
 * kvpack (github.com/pablo-botella/kvpack) is a tiny binary format that
 * packs the data of a call (a request or its reply) between programs in Go,
 * C and Harbour: a command number plus a flat list of typed key/value items,
 * with a crc32 over the payload. This file and kvpack.c implement it with
 * nothing beyond the C standard library: no allocation (the writer fills a
 * buffer the caller provides, the reader points into the bytes the caller
 * received), ANSI C (C89) plus 64-bit integers, MSVC or gcc, 32 or 64 bit.
 *
 * Wire format (little-endian):
 *
 *   HEADER (20 bytes):
 *     0   magic     "KVP" (3)
 *     3   version   uint8   (1)
 *     4   command   uint32  application command number
 *     8   nitems    uint32  number of items in DATA
 *     12  size      uint32  byte length of DATA
 *     16  crc32     uint32  crc32 (IEEE) of DATA
 *   DATA (size bytes): nitems items, back to back:
 *     key       uint16  application key; 0 is reserved for an error text
 *     type      uint16  KVP_INT32 1, KVP_INT64 2, KVP_TS 3, KVP_TEXT 4, KVP_BIN 5, KVP_MSG 6,
 *                       KVP_BOOL 7, KVP_FLOAT64 8, KVP_DS 9, KVP_NULL 10,
 *                       KVP_TUPLEDEF 11, KVP_TUPLE 12
 *     size      uint32  byte length of value
 *     value     size bytes
 *
 * Value encodings: int32 is 4 bytes, int64 and ts 8 bytes (ts is Unix
 * seconds, UTC, signed), text is UTF-8 without a terminator, bin is raw,
 * msg is a whole message (header included) nested as a value: a record,
 * whose command number says which kind. bool is 1 byte (0 or 1), float64
 * 8 bytes IEEE 754, ds 8 ASCII digits yyyymmdd (a calendar day, as
 * Harbour's DToS writes it), 8 spaces for the null date -- and, as with
 * SToD, anything that is not a real day reads as the null date; in C a ds is an int32_t
 * yyyymmdd, 0 for the null date. null has no value (size 0): the key is
 * there and says "no value", which is not a zero, an empty text or an
 * absent key. A key may repeat (a list). A reply carrying key 0 as text is
 * an error reply.
 *
 * Tuples carry data whose shape is only known when the message is built
 * (the rows of a query): a structure declared on the fly and then repeated.
 * Under one key go a tupledef, the definition, and right after it zero or
 * more tuples, each holding only values in the order of the definition:
 *
 *   tupledef value:  nfields uint32, then per field
 *                    type uint16 | name size uint32 | name | meta size uint32 | meta
 *   tuple value:     per field of the definition, in order
 *                    type uint16 | size uint32 | value
 *
 * The name is UTF-8; the meta is the application's, raw bytes never looked
 * at. A field takes any value type (int32 to ds); a tuple holds no tuples.
 * In a tuple each value says its type again: the one the definition
 * declares for the field, or KVP_NULL (size 0) when the field is null.
 * The order is strict: at most one tupledef per key, a tuple right after
 * its tupledef or another tuple of the same key, and every tuple with
 * exactly the fields of the definition, each of its type and the size that
 * type demands, or null. kvp_parse refuses anything else.
 *
 * Writing:
 *
 *   uint8_t buf[512];
 *   kvp_writer w;
 *   size_t len;
 *   kvp_begin(&w, buf, sizeof buf, CMD_LOGIN);
 *   kvp_add_text(&w, K_USER, "bob");
 *   kvp_add_ts(&w, K_WHEN, (int64_t)time(NULL));
 *   if (kvp_end(&w, &len) == KVP_OK) send(buf, len);
 *
 * Reading:
 *
 *   kvp_message m;
 *   int32_t code;
 *   char name[64];
 *   if (kvp_parse(bytes, n, &m) == KVP_OK) {
 *       kvp_get_int32(&m, K_CODE, &code);
 *       kvp_get_text(&m, K_NAME, name, sizeof name);
 *   }
 *
 * All functions return 0 (KVP_OK) on success or a negative kvp_error.
 */
#ifndef KVPACK_H
#define KVPACK_H

#include <stddef.h>
#if defined(_MSC_VER) && _MSC_VER < 1600
/* Visual Studio before 2010 has no stdint.h. */
typedef signed char      int8_t;
typedef unsigned char    uint8_t;
typedef short            int16_t;
typedef unsigned short   uint16_t;
typedef int              int32_t;
typedef unsigned int     uint32_t;
typedef __int64          int64_t;
typedef unsigned __int64 uint64_t;
#else
#include <stdint.h>
#endif

#ifdef __cplusplus
extern "C" {
#endif

#define KVP_VERSION      1
#define KVP_HEADER_SIZE  20
#define KVP_ITEM_SIZE    8
#define KVP_KEY_ERROR    0     /* reserved key of an error reply (a text) */

/* Item value types. */
#define KVP_INT32  1
#define KVP_INT64  2
#define KVP_TS     3           /* int64: Unix seconds, UTC */
#define KVP_TEXT   4           /* UTF-8, no terminator */
#define KVP_BIN    5
#define KVP_MSG    6           /* a nested message, header included */
#define KVP_BOOL   7           /* 1 byte: 0 or 1 */
#define KVP_FLOAT64 8          /* 8 bytes, IEEE 754 */
#define KVP_DS     9           /* 8 ASCII digits yyyymmdd (DToS); 8 spaces = null date */
#define KVP_NULL   10          /* no value: size 0 */
#define KVP_TUPLEDEF 11        /* the definition of the tuples that follow it under its key */
#define KVP_TUPLE  12          /* a tuple: the values of the fields of its definition */

#define KVP_TUPLE_FIELD_SIZE 6       /* type and size before each value of a tuple */

/* As the size of a field's meta: the meta is a NUL-terminated string, to be
 * measured (the terminator does not travel). It is (uint32_t)-1, which no
 * real meta can measure. */
#define KVP_STRLEN 0xFFFFFFFFu

typedef enum {
    KVP_OK          =  0,
    KVP_E_ARGUMENT  = -1,      /* NULL or otherwise invalid argument */
    KVP_E_MAGIC     = -2,      /* not a message */
    KVP_E_VERSION   = -3,      /* unsupported version */
    KVP_E_TRUNCATED = -4,      /* fewer bytes than the message announces */
    KVP_E_FORMAT    = -5,      /* inconsistent items (count, sizes, types) */
    KVP_E_CRC       = -6,      /* crc32 mismatch */
    KVP_E_FULL      = -7,      /* the writer's buffer (or a text buffer) is too small */
    KVP_E_NOT_FOUND = -8,      /* no item with that key, or end of the walk */
    KVP_E_TYPE      = -9,      /* the item is not of the requested type */
    KVP_E_MISSING   = -10,     /* generated code: a required field is missing */
    KVP_E_REPLY     = -11,     /* generated code: the reply is an error reply */
    KVP_E_CALL      = -12      /* generated code: the transport of a call failed */
} kvp_error;

/* Text and bytes by reference: p points into a received message (or, when
 * writing, at the caller's bytes), len counts bytes. Text is UTF-8 with no
 * terminator. An absent value is { NULL, 0 }. */
typedef struct {
    const char *p;
    uint32_t    len;
} kvp_utf8;

typedef struct {
    const void *p;
    uint32_t    len;
} kvp_bytes;

/* kvp_utf8_of points a kvp_utf8 at a NUL-terminated string (NULL gives
 * { NULL, 0 }). */
kvp_utf8 kvp_utf8_of(const char *s);

/* --- writing ------------------------------------------------------------- */

/* A writer fills a caller's buffer in place: header first, items as they
 * are added, header sealed by kvp_end. Once an add fails (KVP_E_FULL)
 * the writer stays failed and kvp_end reports it, so a sequence of adds
 * can be checked once at the end. */
typedef struct {
    uint8_t *buf;
    size_t   cap;
    size_t   len;               /* bytes used so far, header included */
    uint32_t nitems;
    int      error;             /* first error, or KVP_OK */
    /* tuples: the definition last written (a tuple may follow it) or, in
     * the writer of a tuple, the one its fields are checked against */
    const uint8_t *def;
    uint32_t def_size;
    uint32_t def_pos;
    uint16_t def_key;
} kvp_writer;

/* kvp_room is the buffer size a message with nitems items and
 * value_bytes bytes of values needs. */
size_t kvp_room(uint32_t nitems, size_t value_bytes);

int kvp_begin(kvp_writer *w, void *buf, size_t cap, uint32_t command);
int kvp_add(kvp_writer *w, uint16_t key, uint16_t type, const void *value, uint32_t size);
int kvp_add_int32(kvp_writer *w, uint16_t key, int32_t v);
int kvp_add_int64(kvp_writer *w, uint16_t key, int64_t v);
int kvp_add_ts(kvp_writer *w, uint16_t key, int64_t unix_seconds);
int kvp_add_text(kvp_writer *w, uint16_t key, const char *utf8);   /* strlen, no terminator */
int kvp_add_bin(kvp_writer *w, uint16_t key, const void *bytes, uint32_t size);
/* kvp_add_msg nests a message built by another writer (its buffer and
 * the length kvp_end gave). */
int kvp_add_msg(kvp_writer *w, uint16_t key, const void *msg, size_t len);
int kvp_add_bool(kvp_writer *w, uint16_t key, int v);              /* nonzero is true */
int kvp_add_float64(kvp_writer *w, uint16_t key, double v);
/* kvp_add_ds takes yyyymmdd; 0, or anything that is no calendar day of the
 * years 1..9999, is written as the null date (8 spaces). */
int kvp_add_ds(kvp_writer *w, uint16_t key, int32_t yyyymmdd);
/* kvp_add_null adds a null: the key is present and has no value. */
int kvp_add_null(kvp_writer *w, uint16_t key);
/* kvp_add_utf8 adds a text of t->len bytes (no terminator needed). */
int kvp_add_utf8(kvp_writer *w, uint16_t key, const kvp_utf8 *t);
/* kvp_begin_msg opens a nested message under key, written in place inside
 * w's buffer: sub is a writer of its own (add to it, nest further) and
 * kvp_end_msg closes it into w. Nothing may be added to w in between. */
int kvp_begin_msg(kvp_writer *w, uint16_t key, kvp_writer *sub, uint32_t command);
int kvp_end_msg(kvp_writer *w, kvp_writer *sub);
/* Tuples. A definition is opened under key with kvp_begin_tupledef (def is
 * a writer of its own, used only with kvp_tupledef_add), given its fields
 * in order (type KVP_INT32..KVP_DS, a name, and a meta of meta_size raw
 * bytes that may be NULL, 0 -- or, for a meta that is a NUL-terminated
 * string, KVP_STRLEN as its size) and closed with kvp_end_tupledef. Each tuple
 * is then opened with kvp_begin_tuple under the same key (row is a writer
 * used only with the kvp_tuple_* functions), given one value per field in
 * the order of the definition -- kvp_tuple_null for a null -- and closed
 * with kvp_end_tuple. Nothing else may be added to w while one is open, and
 * the tuples must follow their definition with no other item in between.
 * The writer holds to the order a reader demands: KVP_E_FORMAT for a second
 * definition under a key, a tuple that does not follow its definition, or
 * one with fewer or more values than fields; KVP_E_TYPE for a value of
 * another type than its field. */
int kvp_begin_tupledef(kvp_writer *w, uint16_t key, kvp_writer *def);
int kvp_tupledef_add(kvp_writer *def, uint16_t type, const char *name, const void *meta, uint32_t meta_size);
int kvp_end_tupledef(kvp_writer *w, kvp_writer *def);
int kvp_begin_tuple(kvp_writer *w, uint16_t key, kvp_writer *row);
int kvp_tuple_int32(kvp_writer *row, int32_t v);
int kvp_tuple_int64(kvp_writer *row, int64_t v);
int kvp_tuple_ts(kvp_writer *row, int64_t unix_seconds);
int kvp_tuple_text(kvp_writer *row, const char *utf8);
int kvp_tuple_utf8(kvp_writer *row, const kvp_utf8 *t);
int kvp_tuple_bin(kvp_writer *row, const void *bytes, uint32_t size);
int kvp_tuple_msg(kvp_writer *row, const void *msg, size_t len);
int kvp_tuple_bool(kvp_writer *row, int v);
int kvp_tuple_float64(kvp_writer *row, double v);
int kvp_tuple_ds(kvp_writer *row, int32_t yyyymmdd);
int kvp_tuple_null(kvp_writer *row);
int kvp_end_tuple(kvp_writer *w, kvp_writer *row);
/* kvp_end seals nitems, size and crc32 in the header and gives the total
 * length; after it the buffer holds the message. */
int kvp_end(kvp_writer *w, size_t *len);

/* --- reading ------------------------------------------------------------- */

/* A parsed message: points into the caller's bytes, which must stay valid
 * while it is used. */
typedef struct {
    const uint8_t *buf;         /* the whole message */
    size_t         len;         /* KVP_HEADER_SIZE + size */
    uint32_t       command;
    uint32_t       nitems;
    uint32_t       size;        /* bytes of data */
    uint32_t       crc32;
} kvp_message;

/* An item as seen by the walk: value points into the message. pos is the
 * walk's own state (offset of the item inside data). */
typedef struct {
    uint16_t       key;
    uint16_t       type;
    uint32_t       size;
    const uint8_t *value;
    size_t         pos;
} kvp_item;

/* A tuple definition as read: points into the message. item is the item
 * it came from, where the walk of its tuples starts. */
typedef struct {
    const uint8_t *buf;         /* the tupledef value */
    uint32_t       size;
    uint32_t       nfields;
    kvp_item       item;
} kvp_tupledef;

/* One field of a definition: name and meta point into the message. */
typedef struct {
    uint16_t  type;
    kvp_utf8  name;
    kvp_bytes meta;             /* the application's; may be empty */
} kvp_field;

/* kvp_header reads only the header: magic and version are checked and
 * m->size says how many bytes follow, so a stream reader knows how much to
 * read before calling kvp_parse. Needs KVP_HEADER_SIZE bytes. */
int kvp_header(const void *buf, size_t len, kvp_message *m);
/* kvp_parse validates a whole message: header, every item (count, sizes,
 * types) and the crc32. len must be exactly the message length. */
int kvp_parse(const void *buf, size_t len, kvp_message *m);

/* The walk: kvp_first then kvp_next until KVP_E_NOT_FOUND. kvp_find
 * positions on the first item with key; kvp_find_next on the next one
 * with the same key after it (lists). */
int kvp_first(const kvp_message *m, kvp_item *it);
int kvp_next(const kvp_message *m, kvp_item *it);
int kvp_find(const kvp_message *m, uint16_t key, kvp_item *it);
int kvp_find_next(const kvp_message *m, kvp_item *it);

/* Typed access to an item: KVP_E_TYPE when it is of another type. text
 * copies the value NUL-terminated into dst; when dst is too small the copy
 * is truncated and KVP_E_FULL returned. bin gives a pointer and size into
 * the message. */
int kvp_int32(const kvp_item *it, int32_t *v);
int kvp_int64(const kvp_item *it, int64_t *v);
int kvp_ts(const kvp_item *it, int64_t *unix_seconds);
int kvp_text(const kvp_item *it, char *dst, size_t cap);
int kvp_bin(const kvp_item *it, const void **bytes, uint32_t *size);
/* msg parses the nested message (already validated by the parent's parse)
 * into sub, which points into the parent. */
int kvp_msg(const kvp_item *it, kvp_message *sub);
/* utf8 points t at a text item, no copy; t stays as it was on error. */
int kvp_item_utf8(const kvp_item *it, kvp_utf8 *t);
int kvp_bool(const kvp_item *it, int *v);                          /* 0 or 1 */
int kvp_float64(const kvp_item *it, double *v);
int kvp_ds(const kvp_item *it, int32_t *yyyymmdd);                 /* 0: null date */

/* The same by key (first item with that key): KVP_E_NOT_FOUND when absent. */
int kvp_get_int32(const kvp_message *m, uint16_t key, int32_t *v);
int kvp_get_int64(const kvp_message *m, uint16_t key, int64_t *v);
int kvp_get_ts(const kvp_message *m, uint16_t key, int64_t *unix_seconds);
int kvp_get_text(const kvp_message *m, uint16_t key, char *dst, size_t cap);
int kvp_get_bin(const kvp_message *m, uint16_t key, const void **bytes, uint32_t *size);
int kvp_get_msg(const kvp_message *m, uint16_t key, kvp_message *sub);
int kvp_get_utf8(const kvp_message *m, uint16_t key, kvp_utf8 *t);
int kvp_get_bool(const kvp_message *m, uint16_t key, int *v);
int kvp_get_float64(const kvp_message *m, uint16_t key, double *v);
int kvp_get_ds(const kvp_message *m, uint16_t key, int32_t *yyyymmdd);
/* kvp_is_null says 1 when the first item with key is a null, 0 otherwise
 * (absent is not null). An item of the walk is a null when its type is
 * KVP_NULL; the typed readers give KVP_E_TYPE on it, as on any other type. */
int kvp_is_null(const kvp_message *m, uint16_t key);

/* Reading tuples. kvp_get_tupledef finds the definition under key
 * (KVP_E_NOT_FOUND when the key holds none) and kvp_tupledef_of takes it
 * from a walked item (KVP_E_TYPE when the item is something else). kvp_tupledef_field gives
 * the field at index (0-based; KVP_E_NOT_FOUND past the last). The tuples
 * are walked from the definition's own item:
 *
 *   kvp_tupledef def;
 *   kvp_item row, f;
 *   if (kvp_get_tupledef(&m, K_ROWS, &def) == KVP_OK)
 *       for (row = def.item; kvp_tuple_next(&m, &row) == KVP_OK; )
 *           if (kvp_tuple_field(&def, &row, 0, &f) == KVP_OK) ...
 *
 * kvp_tuple_next moves it to the next item when that is a tuple of the
 * same key, and says KVP_E_NOT_FOUND when the tuples are over.
 * kvp_tuple_field gives the field at index of a tuple as an item, to read
 * with kvp_int32, kvp_item_utf8, kvp_bin... as any other: its key is the
 * position (1-based) and its type the one the definition declares, or
 * KVP_NULL when the field is null (the typed readers then say KVP_E_TYPE). */
int kvp_tupledef_of(const kvp_item *it, kvp_tupledef *def);
int kvp_get_tupledef(const kvp_message *m, uint16_t key, kvp_tupledef *def);
int kvp_tupledef_field(const kvp_tupledef *def, uint32_t index, kvp_field *f);
int kvp_tuple_next(const kvp_message *m, kvp_item *it);
int kvp_tuple_field(const kvp_tupledef *def, const kvp_item *row, uint32_t index, kvp_item *field);

/* kvp_error_text copies the text of an error reply (key KVP_KEY_ERROR)
 * into dst; KVP_E_NOT_FOUND when the message is not an error. kvp_is_error
 * says 1 when it is one. */
int kvp_error_text(const kvp_message *m, char *dst, size_t cap);
int kvp_is_error(const kvp_message *m);

/* --- misc ---------------------------------------------------------------- */

const char *kvp_error_str(int error);
const char *kvp_type_str(uint16_t type);
/* crc32 (IEEE), incremental: start with crc = 0. */
uint32_t kvp_crc32(uint32_t crc, const void *data, size_t len);

#ifdef __cplusplus
}
#endif

#endif /* KVPACK_H */

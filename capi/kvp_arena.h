/*
 * kvp_arena.h — messages as fixed structs over an arena, for callers that
 * cannot size a message in advance. Optional: kvpack.c never allocates and
 * does not need this file. The code kvpack generates from a .kvp is built
 * on it.
 *
 * A message is a struct of fixed size, the caller's to declare, whose
 * members are small records: a value and whether it is null, or -- for what
 * has no fixed size: a text, a list, the tuples of a query -- where its
 * bytes lie in an arena. The arena is a kvp_buffer (dyn_buffer.h), a single
 * block that grows through the application's malloc and free; everything
 * of variable size is copied into it and referred to by offset, never by
 * pointer, because the block moves when it grows. One kvp_buffer_free
 * releases the whole message.
 *
 *   kvp_buffer a;
 *   api_login s;
 *   kvp_buffer_init(&a, malloc, free, 0, 0);
 *   memset(&s, 0, sizeof s);                     everything empty, nothing null
 *   s.when.value = now;                          a scalar: just assign
 *   kvp_set_text(&a, &s.user, "bob");            variable: copied into the arena
 *   s.password.null = 1;                         a null
 *   n = api_login_size(&s, &a);                  the exact size of the message
 *   buf = malloc(n);                             the caller's, as always
 *   api_login_encode(&s, &a, buf, n, &len);
 *   kvp_buffer_free(&a);
 *
 * Reading goes the same way round: the bytes received are the buffer (wrap
 * them with kvp_buffer_wrap, or receive straight into a kvp_buffer), and the
 * decoded struct refers to them by offset, with nothing copied:
 *
 *   kvp_buffer b;
 *   api_login_reply r;
 *   kvp_buffer_wrap(&b, bytes, n);
 *   if (api_login_reply_decode(&b, &r) == KVP_OK && !r.name.null)
 *       use(kvp_st_ptr(&b, &r.name), r.name.size);
 *
 * A struct built holds its lists and tuples in the arena; one decoded holds
 * how many arrived, and they are walked over the message (the generated X_F
 * functions, and kvp_tupleset_open). A decoded struct can be encoded again,
 * with the buffer it was decoded from: what arrived is written as it
 * arrived. Set a struct to zero before building on it.
 *
 * ANSI C (C89), as kvpack.h.
 */
#ifndef KVP_ARENA_H
#define KVP_ARENA_H

#include "dyn_buffer.h"

#ifdef __cplusplus
extern "C" {
#endif

/* An offset into a kvp_buffer. 0 is "none": nothing the arena hands out
 * lies at offset 0. */
typedef uint32_t kvp_off;

/* What a .kvp declares of a field, for the functions that write and read
 * it. A field with neither is always written. */
#define KVP_OPTIONAL 1          /* written when it is not empty */
#define KVP_NULLABLE 2          /* written always, as its value or as a null;
                                   with KVP_OPTIONAL: written when it is not null */

/* --- the records --------------------------------------------------------- */

/* One record per field, of the same shape whatever the field declares: its
 * value and null, 1 when the field is null (value is then not looked at).
 * A struct set to zero has every field empty and none null. null only
 * travels for a nullable field: a reader sets it, a writer looks at it,
 * only then. */
typedef struct { int null; int32_t value; } kvp_int32_st;
typedef struct { int null; int64_t value; } kvp_int64_st;
typedef kvp_int64_st kvp_ts_st;                 /* Unix seconds, UTC */
typedef struct { int null; int     value; } kvp_bool_st;       /* 0 or 1 */
typedef struct { int null; double  value; } kvp_float64_st;
typedef kvp_int32_st kvp_ds_st;                 /* yyyymmdd; 0: the null date */

/* Text, bin and a nested message as raw bytes: size bytes at off in the
 * buffer -- the arena for a struct being built, the message for one
 * decoded. Text is UTF-8 with no terminator. */
typedef struct {
    int      null;
    uint32_t size;
    kvp_off  off;
} kvp_text_st;
typedef kvp_text_st kvp_bin_st;
typedef kvp_text_st kvp_msg_st;

/* A list. count says how many elements it has, added (built) or arrived
 * (decoded); first and last are the arena's chain of a built one. */
typedef struct {
    kvp_off  first;
    kvp_off  last;
    uint32_t count;
} kvp_list_st;

/* A tupleset: a definition and its tuples, whose shape is only known when
 * the message is built. nfields and ntuples say how many there are, added
 * or arrived; the rest is the arena's, for one being built. */
typedef struct {
    uint32_t nfields;
    uint32_t ntuples;
    kvp_off  def;               /* the fields, chained */
    kvp_off  def_last;
    kvp_off  first;             /* the tuples, chained */
    kvp_off  last;
    kvp_off  open;              /* the tuple being written, 0 when none */
    kvp_off  end;               /* where it ends so far */
    kvp_off  cur;               /* the field its next value belongs to */
    uint32_t filled;            /* values it has so far */
    int      error;             /* first error of the open tuple */
} kvp_tupleset_st;

/* Where the message a struct was decoded from lies in its buffer. size 0:
 * the struct was not decoded. */
typedef struct {
    kvp_off  off;
    uint32_t size;
} kvp_span;

/* --- variable values ----------------------------------------------------- */

/* kvp_set_text copies a NUL-terminated text into the arena and points the
 * field at it, not null; kvp_set_bytes does the same with size bytes, for a
 * text by its length, a bin or an encoded message. The bytes may lie in the
 * arena itself. KVP_E_FULL when there is no memory: the field stays as it
 * was. Setting a field again leaves its former bytes in the arena, unused,
 * until the arena is freed. */
int kvp_set_text(kvp_buffer *a, kvp_text_st *f, const char *utf8);
int kvp_set_bytes(kvp_buffer *a, kvp_text_st *f, const void *bytes, uint32_t size);
/* kvp_st_ptr gives the f->size bytes of a field, NULL when it is null or
 * empty; kvp_st_utf8 the same as a { p, len } pair. The pointer is into the
 * buffer and stops being valid when the buffer grows. kvp_st_msg parses a
 * field that holds a nested message. */
const uint8_t *kvp_st_ptr(const kvp_buffer *b, const kvp_text_st *f);
kvp_utf8 kvp_st_utf8(const kvp_buffer *b, const kvp_text_st *f);
int kvp_st_msg(const kvp_buffer *b, const kvp_msg_st *f, kvp_message *m);

/* --- lists --------------------------------------------------------------- */

/* kvp_list_add appends one element to a list being built: a value of that
 * type, copied into the arena (the typed ones encode it first). The
 * generated X_add_F functions call these. kvp_list_reserve and
 * kvp_list_link do it in two steps, for an element written in place: room
 * for size bytes in a node not yet in any list (kvp_list_value points at
 * them), then the node added to the list. */
int kvp_list_add(kvp_buffer *a, kvp_list_st *l, uint16_t type, const void *value, uint32_t size);
int kvp_list_add_int32(kvp_buffer *a, kvp_list_st *l, int32_t v);
int kvp_list_add_int64(kvp_buffer *a, kvp_list_st *l, int64_t v);
int kvp_list_add_ts(kvp_buffer *a, kvp_list_st *l, int64_t unix_seconds);
int kvp_list_add_bool(kvp_buffer *a, kvp_list_st *l, int v);
int kvp_list_add_float64(kvp_buffer *a, kvp_list_st *l, double v);
int kvp_list_add_ds(kvp_buffer *a, kvp_list_st *l, int32_t yyyymmdd);
int kvp_list_reserve(kvp_buffer *a, uint16_t type, uint32_t size, kvp_off *node);
uint8_t *kvp_list_value(const kvp_buffer *a, kvp_off node);
int kvp_list_link(kvp_buffer *a, kvp_list_st *l, kvp_off node);

/* --- tuplesets ----------------------------------------------------------- */

/* Building one, on a kvp_tupleset_st set to zero. First the definition:
 * kvp_tupleset_field once per field, in order (type KVP_INT32..KVP_DS, a
 * name, and a meta of meta_size raw bytes, the application's, that may be
 * NULL, 0 -- or, for a meta that is a NUL-terminated string, KVP_STRLEN as
 * its size). Then each tuple: kvp_tupleset_begin, one value per field in the
 * order of the definition -- kvp_tupleset_null for a null -- and
 * kvp_tupleset_end. Everything is copied into the arena.
 *
 *   kvp_tupleset_field(&a, &r.rows, KVP_INT32, "id", NULL, 0);
 *   kvp_tupleset_field(&a, &r.rows, KVP_TEXT, "name", NULL, 0);
 *   while (next_row()) {
 *       kvp_tupleset_begin(&a, &r.rows);
 *       kvp_tupleset_int32(&a, &r.rows, id);
 *       kvp_tupleset_text(&a, &r.rows, name);
 *       if (kvp_tupleset_end(&a, &r.rows) != KVP_OK) ...
 *   }
 *
 * Once a value fails the tuple stays failed and kvp_tupleset_end reports
 * it and drops the tuple, so a run of values is checked once. The errors
 * are the ones a reader would refuse the message for: KVP_E_TYPE for a
 * field type that is none of the value types or a value of another type
 * than its field, KVP_E_ARGUMENT for a field with no name or a repeated
 * one, KVP_E_FORMAT for a field added after the first tuple, a tuple
 * begun while another is open, more or fewer values than fields (a
 * tupleset of no fields takes tuples of no values) -- and for anything else
 * put into the arena while a tuple is open -- and KVP_E_FULL when there is
 * no memory. */
int kvp_tupleset_field(kvp_buffer *a, kvp_tupleset_st *t, uint16_t type, const char *name, const void *meta, uint32_t meta_size);
int kvp_tupleset_begin(kvp_buffer *a, kvp_tupleset_st *t);
int kvp_tupleset_int32(kvp_buffer *a, kvp_tupleset_st *t, int32_t v);
int kvp_tupleset_int64(kvp_buffer *a, kvp_tupleset_st *t, int64_t v);
int kvp_tupleset_ts(kvp_buffer *a, kvp_tupleset_st *t, int64_t unix_seconds);
int kvp_tupleset_text(kvp_buffer *a, kvp_tupleset_st *t, const char *utf8);
int kvp_tupleset_utf8(kvp_buffer *a, kvp_tupleset_st *t, const void *utf8, uint32_t size);
int kvp_tupleset_bin(kvp_buffer *a, kvp_tupleset_st *t, const void *bytes, uint32_t size);
int kvp_tupleset_msg(kvp_buffer *a, kvp_tupleset_st *t, const void *msg, size_t len);
int kvp_tupleset_bool(kvp_buffer *a, kvp_tupleset_st *t, int v);
int kvp_tupleset_float64(kvp_buffer *a, kvp_tupleset_st *t, double v);
int kvp_tupleset_ds(kvp_buffer *a, kvp_tupleset_st *t, int32_t yyyymmdd);
int kvp_tupleset_null(kvp_buffer *a, kvp_tupleset_st *t);
int kvp_tupleset_end(kvp_buffer *a, kvp_tupleset_st *t);

/* Reading one, from a decoded struct: kvp_tupleset_open gives the message
 * the struct was read from (its kvp member) and the definition under key,
 * to walk with the functions of kvpack.h. KVP_E_NOT_FOUND when the key
 * holds no definition.
 *
 *   kvp_message m;
 *   kvp_tupledef def;
 *   kvp_item row, f;
 *   if (kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK)
 *       for (row = def.item; kvp_tuple_next(&m, &row) == KVP_OK; )
 *           if (kvp_tuple_field(&def, &row, 0, &f) == KVP_OK) ...
 */
int kvp_tupleset_open(const kvp_buffer *b, const kvp_span *msg, uint16_t key, kvp_message *m, kvp_tupledef *def);

/* --- writing a struct: what the generated code is made of ---------------- */

/* A kvp_out takes the fields of a struct one after another and either
 * writes them into the caller's buffer or only adds up what they take, so
 * the same generated function gives the size of a message and the message:
 *
 *   kvp_out_measure(&o);  X_put(&o, a, s);  size = o.size;
 *   kvp_out_begin(&o, buf, cap, command);  X_put(&o, a, s);  kvp_out_end(&o, &len);
 */
typedef struct {
    kvp_writer w;
    int        live;            /* 1: writing; 0: only measuring */
    size_t     size;            /* bytes of the message so far, header included */
} kvp_out;

void kvp_out_measure(kvp_out *o);
void kvp_out_begin(kvp_out *o, void *buf, size_t cap, uint32_t command);
int  kvp_out_end(kvp_out *o, size_t *len);

/* kvp_put_* write one field under key as its flags say: a field that is
 * neither optional nor nullable always, an optional one when it is not
 * empty, a nullable one always (a null when f->null), an optional nullable
 * one when it is not null. A nested message of no bytes is the empty one:
 * where a message is written, it goes as a message of command 0 and no
 * items. */
void kvp_put_int32(kvp_out *o, uint16_t key, int flags, const kvp_int32_st *f);
void kvp_put_int64(kvp_out *o, uint16_t key, int flags, const kvp_int64_st *f);
void kvp_put_ts(kvp_out *o, uint16_t key, int flags, const kvp_ts_st *f);
void kvp_put_bool(kvp_out *o, uint16_t key, int flags, const kvp_bool_st *f);
void kvp_put_float64(kvp_out *o, uint16_t key, int flags, const kvp_float64_st *f);
void kvp_put_ds(kvp_out *o, uint16_t key, int flags, const kvp_ds_st *f);
void kvp_put_bytes(kvp_out *o, const kvp_buffer *a, uint16_t key, uint16_t type, int flags, const kvp_text_st *f);
/* kvp_put_begin opens a record field: when it returns 1 the record is to be
 * written into sub and closed with kvp_put_end; when 0 there is nothing to
 * write (it is left out, or went as a null). has says whether an optional
 * record is there. */
int  kvp_put_begin(kvp_out *o, uint16_t key, int flags, int null, int has, kvp_out *sub, uint32_t command);
void kvp_put_end(kvp_out *o, kvp_out *sub);
/* The elements of a list and a tupleset. src is the struct's kvp member:
 * what arrived in the message a decoded struct was read from is written
 * again as it arrived (the elements of a list that are of that type; the
 * definition and its tuples), and then what was added in the arena. A
 * tupleset with no fields is the empty one: left out when optional, written
 * as a definition of no fields when not. */
void kvp_put_list(kvp_out *o, const kvp_buffer *a, uint16_t key, uint16_t type, const kvp_span *src, const kvp_list_st *l);
void kvp_put_tupleset(kvp_out *o, const kvp_buffer *a, uint16_t key, int flags, const kvp_span *src, const kvp_tupleset_st *t);

/* --- reading a struct: what the generated code is made of ---------------- */

/* kvp_read_* fill one field from the first item with key in a parsed
 * message, which lies in b. KVP_E_MISSING when a field that is not optional
 * did not arrive, or arrived as another type; an optional one then reads
 * empty, an optional nullable one null. */
int kvp_read_int32(const kvp_message *m, uint16_t key, int flags, kvp_int32_st *f);
int kvp_read_int64(const kvp_message *m, uint16_t key, int flags, kvp_int64_st *f);
int kvp_read_ts(const kvp_message *m, uint16_t key, int flags, kvp_ts_st *f);
int kvp_read_bool(const kvp_message *m, uint16_t key, int flags, kvp_bool_st *f);
int kvp_read_float64(const kvp_message *m, uint16_t key, int flags, kvp_float64_st *f);
int kvp_read_ds(const kvp_message *m, uint16_t key, int flags, kvp_ds_st *f);
int kvp_read_bytes(const kvp_buffer *b, const kvp_message *m, uint16_t key, uint16_t type, int flags, kvp_text_st *f);
/* kvp_read_rec finds a record field: *has is 1 when it arrived, and sub is
 * then its message, for the record's own X_read. */
int kvp_read_rec(const kvp_message *m, uint16_t key, int flags, int *null, int *has, kvp_message *sub);
/* kvp_read_list counts the elements that arrived; kvp_read_tupleset the
 * fields and tuples of the definition under key. kvp_read_item turns a
 * walked item of that type into a field. kvp_span_of notes where m lies in
 * b, and kvp_span_message gives it back. */
void kvp_read_list(const kvp_message *m, uint16_t key, uint16_t type, kvp_list_st *l);
int kvp_read_tupleset(const kvp_message *m, uint16_t key, int flags, kvp_tupleset_st *t);
int kvp_read_item(const kvp_buffer *b, const kvp_item *it, uint16_t type, kvp_text_st *f);
void kvp_span_of(const kvp_buffer *b, const kvp_message *m, kvp_span *s);
int kvp_span_message(const kvp_buffer *b, const kvp_span *s, kvp_message *m);

#ifdef __cplusplus
}
#endif

#endif /* KVP_ARENA_H */

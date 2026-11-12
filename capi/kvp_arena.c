/*
 * kvp_arena.c — messages as fixed structs over an arena. See kvp_arena.h.
 */
#include "kvp_arena.h"

#include <string.h>

/* What the arena chains. Every node starts with the offset of the next
 * one, 0 in the last:
 *
 *   list element:  next u32 | type u16 | size u32 | value
 *   field:         next u32 | type u16 | name size u32 | meta size u32 | name | NUL | meta
 *   tuple:         next u32 | size u32 | value, as it travels
 */
#define NODE_LIST  10
#define NODE_FIELD 14
#define NODE_TUPLE 8

#define NOWHERE ((size_t)-1)

static uint16_t rd16(const uint8_t *p)
{
    return (uint16_t)(p[0] | (p[1] << 8));
}

static uint32_t rd32(const uint8_t *p)
{
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

static void wr16(uint8_t *p, uint16_t v)
{
    p[0] = (uint8_t)v;
    p[1] = (uint8_t)(v >> 8);
}

static void wr32(uint8_t *p, uint32_t v)
{
    p[0] = (uint8_t)v;
    p[1] = (uint8_t)(v >> 8);
    p[2] = (uint8_t)(v >> 16);
    p[3] = (uint8_t)(v >> 24);
}

/* at gives the n bytes in use at off, or NULL when they are not all there
 * (and for offset 0, which is no offset). */
static uint8_t *at(const kvp_buffer *b, kvp_off off, size_t n)
{
    size_t max = 0;
    uint8_t *p = kvp_buffer_getptr_at(b, off, &max);
    return (p && off && max >= n) ? p : NULL;
}

/* inside says where in the arena p points, NOWHERE when it does not. */
static size_t inside(const kvp_buffer *a, const void *p)
{
    const uint8_t *q = (const uint8_t *)p;
    if (!a || !a->data || !q || q < a->data || q >= a->data + a->size)
        return NOWHERE;
    return (size_t)(q - a->data);
}

/* take allocates head + n bytes at the end of the arena and copies src
 * into the last n; the head is left to the caller. src may lie in the arena
 * itself, which moves when it grows. */
static int take(kvp_buffer *a, size_t head, const void *src, size_t n, kvp_off *off)
{
    size_t from = inside(a, src), start, need = head + n;
    uint8_t *p;
    if (!a || !off || (!src && n))
        return KVP_E_ARGUMENT;
    if (a->size == 0 && kvp_buffer_append(a, "", 1) != KVP_OK)     /* nothing lies at offset 0 */
        return KVP_E_FULL;
    start = a->size;
    if (need < n || start + need < start || start + need > 0xFFFFFFFFu)
        return KVP_E_FULL;
    p = kvp_buffer_lock_bytes(a, need);
    if (!p)
        return KVP_E_FULL;
    if (n)
        memcpy(p + head, from != NOWHERE ? a->data + from : (const uint8_t *)src, n);
    kvp_buffer_unlock_bytes(a, need);
    *off = (kvp_off)start;
    return KVP_OK;
}

/* A scalar is encoded by kvpack.c itself, through a writer over a scratch
 * buffer: the value of its one item is what the arena keeps. */
typedef struct {
    kvp_writer w;
    uint8_t    b[KVP_HEADER_SIZE + KVP_ITEM_SIZE + 8];
} scratch;

#define SCRATCH_VALUE(s) ((s)->b + KVP_HEADER_SIZE + KVP_ITEM_SIZE)

static void scratch_begin(scratch *s)
{
    kvp_begin(&s->w, s->b, sizeof s->b, 0);
}

/* --- variable values ----------------------------------------------------- */

int kvp_set_bytes(kvp_buffer *a, kvp_text_st *f, const void *bytes, uint32_t size)
{
    kvp_off off = 0;
    int rc;
    if (!f || (!bytes && size))
        return KVP_E_ARGUMENT;
    if (size) {
        rc = take(a, 0, bytes, size, &off);
        if (rc != KVP_OK)
            return rc;
    }
    f->null = 0;
    f->size = size;
    f->off = off;
    return KVP_OK;
}

int kvp_set_text(kvp_buffer *a, kvp_text_st *f, const char *utf8)
{
    size_t n;
    if (!utf8)
        return KVP_E_ARGUMENT;
    n = strlen(utf8);
    if (n > 0xFFFFFFFFu)
        return KVP_E_FULL;
    return kvp_set_bytes(a, f, utf8, (uint32_t)n);
}

const uint8_t *kvp_st_ptr(const kvp_buffer *b, const kvp_text_st *f)
{
    if (!f || f->null || !f->size)
        return NULL;
    return at(b, f->off, f->size);
}

kvp_utf8 kvp_st_utf8(const kvp_buffer *b, const kvp_text_st *f)
{
    kvp_utf8 t;
    t.p = (const char *)kvp_st_ptr(b, f);
    t.len = t.p ? f->size : 0;
    return t;
}

int kvp_st_msg(const kvp_buffer *b, const kvp_msg_st *f, kvp_message *m)
{
    const uint8_t *p = kvp_st_ptr(b, f);
    if (!m)
        return KVP_E_ARGUMENT;
    if (!p)
        return KVP_E_NOT_FOUND;
    return kvp_parse(p, f->size, m);
}

/* --- lists --------------------------------------------------------------- */

static void node_head(uint8_t *p, uint16_t type, uint32_t size)
{
    wr32(p, 0);
    wr16(p + 4, type);
    wr32(p + 6, size);
}

int kvp_list_reserve(kvp_buffer *a, uint16_t type, uint32_t size, kvp_off *node)
{
    int rc = take(a, NODE_LIST + (size_t)size, NULL, 0, node);
    if (rc == KVP_OK)
        node_head(a->data + *node, type, size);
    return rc;
}

uint8_t *kvp_list_value(const kvp_buffer *a, kvp_off node)
{
    uint8_t *p = at(a, node, NODE_LIST);
    if (!p || !at(a, node, NODE_LIST + (size_t)rd32(p + 6)))
        return NULL;
    return p + NODE_LIST;
}

int kvp_list_link(kvp_buffer *a, kvp_list_st *l, kvp_off node)
{
    uint8_t *last;
    if (!l || !at(a, node, NODE_LIST))
        return KVP_E_ARGUMENT;
    if (l->last) {
        last = at(a, l->last, NODE_LIST);
        if (!last)
            return KVP_E_ARGUMENT;
        wr32(last, node);
    } else {
        l->first = node;
    }
    l->last = node;
    l->count++;
    return KVP_OK;
}

int kvp_list_add(kvp_buffer *a, kvp_list_st *l, uint16_t type, const void *value, uint32_t size)
{
    kvp_off node;
    int rc;
    if (!l)
        return KVP_E_ARGUMENT;
    rc = take(a, NODE_LIST, value, size, &node);
    if (rc != KVP_OK)
        return rc;
    node_head(a->data + node, type, size);
    return kvp_list_link(a, l, node);
}

int kvp_list_add_int32(kvp_buffer *a, kvp_list_st *l, int32_t v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_int32(&s.w, 1, v);
    return kvp_list_add(a, l, KVP_INT32, SCRATCH_VALUE(&s), 4);
}

int kvp_list_add_int64(kvp_buffer *a, kvp_list_st *l, int64_t v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_int64(&s.w, 1, v);
    return kvp_list_add(a, l, KVP_INT64, SCRATCH_VALUE(&s), 8);
}

int kvp_list_add_ts(kvp_buffer *a, kvp_list_st *l, int64_t unix_seconds)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_ts(&s.w, 1, unix_seconds);
    return kvp_list_add(a, l, KVP_TS, SCRATCH_VALUE(&s), 8);
}

int kvp_list_add_bool(kvp_buffer *a, kvp_list_st *l, int v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_bool(&s.w, 1, v);
    return kvp_list_add(a, l, KVP_BOOL, SCRATCH_VALUE(&s), 1);
}

int kvp_list_add_float64(kvp_buffer *a, kvp_list_st *l, double v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_float64(&s.w, 1, v);
    return kvp_list_add(a, l, KVP_FLOAT64, SCRATCH_VALUE(&s), 8);
}

int kvp_list_add_ds(kvp_buffer *a, kvp_list_st *l, int32_t yyyymmdd)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_ds(&s.w, 1, yyyymmdd);
    return kvp_list_add(a, l, KVP_DS, SCRATCH_VALUE(&s), 8);
}

/* --- tuplesets: building ------------------------------------------------- */

int kvp_tupleset_field(kvp_buffer *a, kvp_tupleset_st *t, uint16_t type, const char *name, const void *meta, uint32_t meta_size)
{
    size_t n, name_in;
    kvp_off off, f;
    uint8_t *p;
    int rc;
    if (meta_size == KVP_STRLEN) {              /* a NUL-terminated meta: measure it */
        n = meta ? strlen((const char *)meta) : 0;
        if (n >= KVP_STRLEN)
            return KVP_E_FULL;
        meta_size = (uint32_t)n;
    }
    if (!a || !t || !name || !name[0] || (!meta && meta_size))
        return KVP_E_ARGUMENT;
    if (type < KVP_INT32 || type > KVP_DS)
        return KVP_E_TYPE;
    if (t->open || t->ntuples)                  /* the definition is closed by its first tuple */
        return KVP_E_FORMAT;
    n = strlen(name);
    if (n > 0xFFFFFFFFu)
        return KVP_E_FULL;
    for (f = t->def; f; f = rd32(p)) {
        p = at(a, f, NODE_FIELD);
        if (!p || !at(a, f, NODE_FIELD + (size_t)rd32(p + 6)))
            return KVP_E_ARGUMENT;
        if (rd32(p + 6) == n && memcmp(p + NODE_FIELD, name, n) == 0)
            return KVP_E_ARGUMENT;              /* a repeated name */
    }
    name_in = inside(a, name);
    rc = take(a, NODE_FIELD + n + 1, meta, meta_size, &off);
    if (rc != KVP_OK)
        return rc;
    p = a->data + off;
    wr32(p, 0);
    wr16(p + 4, type);
    wr32(p + 6, (uint32_t)n);
    wr32(p + 10, meta_size);
    memcpy(p + NODE_FIELD, name_in != NOWHERE ? (const char *)a->data + name_in : name, n);
    p[NODE_FIELD + n] = 0;
    if (t->def_last)
        wr32(a->data + t->def_last, off);
    else
        t->def = off;
    t->def_last = off;
    t->nfields++;
    return KVP_OK;
}

int kvp_tupleset_begin(kvp_buffer *a, kvp_tupleset_st *t)
{
    kvp_off off;
    int rc;
    if (!a || !t)
        return KVP_E_ARGUMENT;
    if (t->open)                                /* no fields: a tuple of no values */
        return KVP_E_FORMAT;
    rc = take(a, NODE_TUPLE, NULL, 0, &off);
    if (rc != KVP_OK)
        return rc;
    wr32(a->data + off, 0);
    wr32(a->data + off + 4, 0);
    t->open = off;
    t->end = off + NODE_TUPLE;
    t->cur = t->def;
    t->filled = 0;
    t->error = KVP_OK;
    return KVP_OK;
}

/* value writes the next value of the open tuple: its type, size and bytes,
 * or the type KVP_NULL with none. */
static int value(kvp_buffer *a, kvp_tupleset_st *t, uint16_t type, const void *bytes, uint32_t size)
{
    const uint8_t *field;
    kvp_off off, next;
    int rc;
    if (!a || !t)
        return KVP_E_ARGUMENT;
    if (!t->open)
        return KVP_E_FORMAT;
    if (t->error)
        return t->error;
    if (!bytes && size)
        return t->error = KVP_E_ARGUMENT;
    if (a->size != t->end || t->filled >= t->nfields)   /* something else got in, or more values than fields */
        return t->error = KVP_E_FORMAT;
    field = at(a, t->cur, NODE_FIELD);
    if (!field)
        return t->error = KVP_E_ARGUMENT;
    if (type != KVP_NULL && type != rd16(field + 4))
        return t->error = KVP_E_TYPE;
    next = rd32(field);
    rc = take(a, KVP_TUPLE_FIELD_SIZE, bytes, size, &off);
    if (rc != KVP_OK)
        return t->error = rc;
    wr16(a->data + off, type);
    wr32(a->data + off + 2, size);
    t->end = (kvp_off)a->size;
    t->cur = next;
    t->filled++;
    return KVP_OK;
}

int kvp_tupleset_int32(kvp_buffer *a, kvp_tupleset_st *t, int32_t v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_int32(&s.w, 1, v);
    return value(a, t, KVP_INT32, SCRATCH_VALUE(&s), 4);
}

int kvp_tupleset_int64(kvp_buffer *a, kvp_tupleset_st *t, int64_t v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_int64(&s.w, 1, v);
    return value(a, t, KVP_INT64, SCRATCH_VALUE(&s), 8);
}

int kvp_tupleset_ts(kvp_buffer *a, kvp_tupleset_st *t, int64_t unix_seconds)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_ts(&s.w, 1, unix_seconds);
    return value(a, t, KVP_TS, SCRATCH_VALUE(&s), 8);
}

int kvp_tupleset_text(kvp_buffer *a, kvp_tupleset_st *t, const char *utf8)
{
    size_t n = utf8 ? strlen(utf8) : 0;
    if (!utf8 || n > 0xFFFFFFFFu)
        return t && t->open ? (t->error = KVP_E_ARGUMENT) : KVP_E_ARGUMENT;
    return value(a, t, KVP_TEXT, utf8, (uint32_t)n);
}

int kvp_tupleset_utf8(kvp_buffer *a, kvp_tupleset_st *t, const void *utf8, uint32_t size)
{
    return value(a, t, KVP_TEXT, size ? utf8 : "", size);
}

int kvp_tupleset_bin(kvp_buffer *a, kvp_tupleset_st *t, const void *bytes, uint32_t size)
{
    return value(a, t, KVP_BIN, size ? bytes : "", size);
}

int kvp_tupleset_msg(kvp_buffer *a, kvp_tupleset_st *t, const void *msg, size_t len)
{
    if (!msg || len > 0xFFFFFFFFu)
        return t && t->open ? (t->error = KVP_E_ARGUMENT) : KVP_E_ARGUMENT;
    return value(a, t, KVP_MSG, msg, (uint32_t)len);
}

int kvp_tupleset_bool(kvp_buffer *a, kvp_tupleset_st *t, int v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_bool(&s.w, 1, v);
    return value(a, t, KVP_BOOL, SCRATCH_VALUE(&s), 1);
}

int kvp_tupleset_float64(kvp_buffer *a, kvp_tupleset_st *t, double v)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_float64(&s.w, 1, v);
    return value(a, t, KVP_FLOAT64, SCRATCH_VALUE(&s), 8);
}

int kvp_tupleset_ds(kvp_buffer *a, kvp_tupleset_st *t, int32_t yyyymmdd)
{
    scratch s;
    scratch_begin(&s);
    kvp_add_ds(&s.w, 1, yyyymmdd);
    return value(a, t, KVP_DS, SCRATCH_VALUE(&s), 8);
}

int kvp_tupleset_null(kvp_buffer *a, kvp_tupleset_st *t)
{
    return value(a, t, KVP_NULL, NULL, 0);
}

int kvp_tupleset_end(kvp_buffer *a, kvp_tupleset_st *t)
{
    kvp_off off;
    uint8_t *p;
    if (!a || !t)
        return KVP_E_ARGUMENT;
    if (!t->open)
        return KVP_E_FORMAT;
    off = t->open;
    t->open = 0;                                /* closed either way: a failed tuple is dropped */
    if (t->error)
        return t->error;
    if (t->filled != t->nfields || a->size != t->end)
        return KVP_E_FORMAT;
    p = at(a, off, NODE_TUPLE);
    if (!p)
        return KVP_E_ARGUMENT;
    wr32(p + 4, t->end - off - NODE_TUPLE);
    if (t->last)
        wr32(a->data + t->last, off);
    else
        t->first = off;
    t->last = off;
    t->ntuples++;
    return KVP_OK;
}

/* --- writing a struct ---------------------------------------------------- */

void kvp_out_measure(kvp_out *o)
{
    if (!o)
        return;
    memset(o, 0, sizeof *o);
    o->size = KVP_HEADER_SIZE;
}

void kvp_out_begin(kvp_out *o, void *buf, size_t cap, uint32_t command)
{
    if (!o)
        return;
    memset(o, 0, sizeof *o);
    o->live = 1;
    o->size = KVP_HEADER_SIZE;
    kvp_begin(&o->w, buf, cap, command);
}

int kvp_out_end(kvp_out *o, size_t *len)
{
    if (!o)
        return KVP_E_ARGUMENT;
    if (o->live)
        return kvp_end(&o->w, len);
    if (len)
        *len = o->size;
    return KVP_OK;
}

/* bad marks a struct that cannot be written: an offset that is not in the
 * arena. */
static void bad(kvp_out *o)
{
    if (o->live && o->w.error == KVP_OK)
        o->w.error = KVP_E_ARGUMENT;
}

#define PUT_NOTHING 0
#define PUT_VALUE   1
#define PUT_NULL    2

/* what applies the rule of a field's flags. */
static int what(int flags, int null, int empty)
{
    if (flags & KVP_NULLABLE) {
        if (!null)
            return PUT_VALUE;
        return (flags & KVP_OPTIONAL) ? PUT_NOTHING : PUT_NULL;
    }
    return ((flags & KVP_OPTIONAL) && empty) ? PUT_NOTHING : PUT_VALUE;
}

static void put_null(kvp_out *o, uint16_t key)
{
    if (o->live)
        kvp_add_null(&o->w, key);
    o->size += KVP_ITEM_SIZE;
}

void kvp_put_int32(kvp_out *o, uint16_t key, int flags, const kvp_int32_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_int32(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 4;
    }
}

void kvp_put_int64(kvp_out *o, uint16_t key, int flags, const kvp_int64_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_int64(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 8;
    }
}

void kvp_put_ts(kvp_out *o, uint16_t key, int flags, const kvp_ts_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_ts(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 8;
    }
}

void kvp_put_bool(kvp_out *o, uint16_t key, int flags, const kvp_bool_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_bool(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 1;
    }
}

void kvp_put_float64(kvp_out *o, uint16_t key, int flags, const kvp_float64_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_float64(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 8;
    }
}

void kvp_put_ds(kvp_out *o, uint16_t key, int flags, const kvp_ds_st *f)
{
    int w = what(flags, f->null, f->value == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
    } else if (w == PUT_VALUE) {
        if (o->live)
            kvp_add_ds(&o->w, key, f->value);
        o->size += KVP_ITEM_SIZE + 8;
    }
}

void kvp_put_bytes(kvp_out *o, const kvp_buffer *a, uint16_t key, uint16_t type, int flags, const kvp_text_st *f)
{
    const uint8_t *p = NULL;
    int w = what(flags, f->null, f->size == 0);
    if (w == PUT_NULL) {
        put_null(o, key);
        return;
    }
    if (w == PUT_NOTHING)
        return;
    if (type == KVP_MSG && f->size == 0) {      /* no bytes: the empty message, command 0 */
        kvp_writer sub;
        if (o->live) {
            kvp_begin_msg(&o->w, key, &sub, 0);
            kvp_end_msg(&o->w, &sub);
        }
        o->size += KVP_ITEM_SIZE + KVP_HEADER_SIZE;
        return;
    }
    if (f->size) {
        p = at(a, f->off, f->size);
        if (!p) {
            bad(o);
            return;
        }
    }
    if (o->live)
        kvp_add(&o->w, key, type, p, f->size);
    o->size += KVP_ITEM_SIZE + (size_t)f->size;
}

int kvp_put_begin(kvp_out *o, uint16_t key, int flags, int null, int has, kvp_out *sub, uint32_t command)
{
    int w = what(flags, null, !has);
    memset(sub, 0, sizeof *sub);
    if (w == PUT_NULL)
        put_null(o, key);
    if (w != PUT_VALUE)
        return 0;
    sub->size = KVP_HEADER_SIZE;
    if (o->live) {
        sub->live = 1;
        kvp_begin_msg(&o->w, key, &sub->w, command);
    }
    return 1;
}

void kvp_put_end(kvp_out *o, kvp_out *sub)
{
    if (o->live)
        kvp_end_msg(&o->w, &sub->w);
    o->size += KVP_ITEM_SIZE + sub->size;
}

/* source parses the message a decoded struct was read from, which lies in
 * a. 0 when the struct was not decoded (src is then at zero). */
static int source(kvp_out *o, const kvp_buffer *a, const kvp_span *src, kvp_message *m)
{
    if (!src || !src->size)
        return 0;
    if (kvp_span_message(a, src, m) != KVP_OK) {
        bad(o);
        return 0;
    }
    return 1;
}

static void put_item(kvp_out *o, uint16_t key, uint16_t type, const kvp_item *it)
{
    if (o->live)
        kvp_add(&o->w, key, type, it->value, it->size);
    o->size += KVP_ITEM_SIZE + (size_t)it->size;
}

void kvp_put_list(kvp_out *o, const kvp_buffer *a, uint16_t key, uint16_t type, const kvp_span *src, const kvp_list_st *l)
{
    const uint8_t *p;
    kvp_message m;
    kvp_item it;
    kvp_off n;
    uint32_t size;
    int rc;
    if (source(o, a, src, &m))                  /* the elements that arrived */
        for (rc = kvp_find(&m, key, &it); rc == KVP_OK; rc = kvp_find_next(&m, &it))
            if (it.type == type)
                put_item(o, key, type, &it);
    for (n = l->first; n; n = rd32(p)) {
        p = at(a, n, NODE_LIST);
        size = p ? rd32(p + 6) : 0;
        if (!p || !at(a, n, NODE_LIST + (size_t)size)) {
            bad(o);
            return;
        }
        if (o->live)
            kvp_add(&o->w, key, rd16(p + 4), p + NODE_LIST, size);
        o->size += KVP_ITEM_SIZE + (size_t)size;
    }
}

void kvp_put_tupleset(kvp_out *o, const kvp_buffer *a, uint16_t key, int flags, const kvp_span *src, const kvp_tupleset_st *t)
{
    const uint8_t *p;
    kvp_writer def;
    kvp_message m;
    kvp_tupledef sdef;
    kvp_item it;
    kvp_off n;
    uint32_t name, meta, size;
    size_t dsize = 4;                           /* the field count */
    if (!t->nfields && (flags & KVP_OPTIONAL))  /* no fields is the empty one */
        return;
    if (!t->def && source(o, a, src, &m) && kvp_get_tupledef(&m, key, &sdef) == KVP_OK) {
        /* the one that arrived, as it arrived */
        if (o->live)
            kvp_add(&o->w, key, KVP_TUPLEDEF, sdef.buf, sdef.size);
        o->size += KVP_ITEM_SIZE + (size_t)sdef.size;
        for (it = sdef.item; kvp_tuple_next(&m, &it) == KVP_OK; )
            put_item(o, key, KVP_TUPLE, &it);
        return;
    }
    memset(&def, 0, sizeof def);
    if (o->live)
        kvp_begin_tupledef(&o->w, key, &def);
    for (n = t->def; n; n = rd32(p)) {
        p = at(a, n, NODE_FIELD);
        name = p ? rd32(p + 6) : 0;
        meta = p ? rd32(p + 10) : 0;
        if (!p || !at(a, n, NODE_FIELD + (size_t)name + 1 + meta)) {
            bad(o);
            return;
        }
        if (o->live)
            kvp_tupledef_add(&def, rd16(p + 4), (const char *)p + NODE_FIELD, p + NODE_FIELD + name + 1, meta);
        dsize += 10 + (size_t)name + meta;
    }
    if (o->live)
        kvp_end_tupledef(&o->w, &def);
    o->size += KVP_ITEM_SIZE + dsize;
    for (n = t->first; n; n = rd32(p)) {
        p = at(a, n, NODE_TUPLE);
        size = p ? rd32(p + 4) : 0;
        if (!p || !at(a, n, NODE_TUPLE + (size_t)size)) {
            bad(o);
            return;
        }
        if (o->live)
            kvp_add(&o->w, key, KVP_TUPLE, p + NODE_TUPLE, size);
        o->size += KVP_ITEM_SIZE + (size_t)size;
    }
}

/* --- reading a struct ---------------------------------------------------- */

/* lookup finds the first item with key. 1: it is a value of that type, in
 * it. 0: there is no value to read and *rc says how that goes -- KVP_OK
 * with *null set for a null (of a nullable field) or for an optional
 * nullable field that did not arrive, KVP_OK for an optional one, and
 * KVP_E_MISSING for the rest. An item of another type counts as absent. */
static int lookup(const kvp_message *m, uint16_t key, uint16_t type, int flags, int *null, kvp_item *it, int *rc)
{
    *null = 0;
    *rc = KVP_OK;
    if (kvp_find(m, key, it) == KVP_OK) {
        if (it->type == type)
            return 1;
        if (it->type == KVP_NULL && (flags & KVP_NULLABLE)) {
            *null = 1;
            return 0;
        }
    }
    if (!(flags & KVP_OPTIONAL))
        *rc = KVP_E_MISSING;
    else if (flags & KVP_NULLABLE)
        *null = 1;
    return 0;
}

int kvp_read_int32(const kvp_message *m, uint16_t key, int flags, kvp_int32_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_INT32, flags, &f->null, &it, &rc))
        kvp_int32(&it, &f->value);
    return rc;
}

int kvp_read_int64(const kvp_message *m, uint16_t key, int flags, kvp_int64_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_INT64, flags, &f->null, &it, &rc))
        kvp_int64(&it, &f->value);
    return rc;
}

int kvp_read_ts(const kvp_message *m, uint16_t key, int flags, kvp_ts_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_TS, flags, &f->null, &it, &rc))
        kvp_ts(&it, &f->value);
    return rc;
}

int kvp_read_bool(const kvp_message *m, uint16_t key, int flags, kvp_bool_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_BOOL, flags, &f->null, &it, &rc))
        kvp_bool(&it, &f->value);
    return rc;
}

int kvp_read_float64(const kvp_message *m, uint16_t key, int flags, kvp_float64_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_FLOAT64, flags, &f->null, &it, &rc))
        kvp_float64(&it, &f->value);
    return rc;
}

int kvp_read_ds(const kvp_message *m, uint16_t key, int flags, kvp_ds_st *f)
{
    kvp_item it;
    int rc;
    f->value = 0;
    if (lookup(m, key, KVP_DS, flags, &f->null, &it, &rc))
        kvp_ds(&it, &f->value);
    return rc;
}

int kvp_read_item(const kvp_buffer *b, const kvp_item *it, uint16_t type, kvp_text_st *f)
{
    size_t off;
    if (!b || !it || !f)
        return KVP_E_ARGUMENT;
    if (it->type != type)
        return KVP_E_TYPE;
    off = inside(b, it->value);
    f->null = 0;
    f->size = 0;
    f->off = 0;
    if (it->size) {
        if (off == NOWHERE || off == 0)
            return KVP_E_ARGUMENT;              /* the item is not of a message in b */
        f->size = it->size;
        f->off = (kvp_off)off;
    }
    return KVP_OK;
}

int kvp_read_bytes(const kvp_buffer *b, const kvp_message *m, uint16_t key, uint16_t type, int flags, kvp_text_st *f)
{
    kvp_item it;
    int rc;
    f->size = 0;
    f->off = 0;
    if (lookup(m, key, type, flags, &f->null, &it, &rc))
        rc = kvp_read_item(b, &it, type, f);
    return rc;
}

int kvp_read_rec(const kvp_message *m, uint16_t key, int flags, int *null, int *has, kvp_message *sub)
{
    kvp_item it;
    int rc;
    *has = 0;
    if (lookup(m, key, KVP_MSG, flags, null, &it, &rc)) {
        rc = kvp_msg(&it, sub);
        *has = rc == KVP_OK;
    }
    return rc;
}

void kvp_read_list(const kvp_message *m, uint16_t key, uint16_t type, kvp_list_st *l)
{
    kvp_item it;
    int rc;
    memset(l, 0, sizeof *l);
    for (rc = kvp_find(m, key, &it); rc == KVP_OK; rc = kvp_find_next(m, &it))
        if (it.type == type)
            l->count++;
}

int kvp_read_tupleset(const kvp_message *m, uint16_t key, int flags, kvp_tupleset_st *t)
{
    kvp_tupledef def;
    kvp_item it;
    memset(t, 0, sizeof *t);
    if (kvp_get_tupledef(m, key, &def) != KVP_OK)
        return (flags & KVP_OPTIONAL) ? KVP_OK : KVP_E_MISSING;
    t->nfields = def.nfields;
    for (it = def.item; kvp_tuple_next(m, &it) == KVP_OK; )
        t->ntuples++;
    return KVP_OK;
}

void kvp_span_of(const kvp_buffer *b, const kvp_message *m, kvp_span *s)
{
    size_t off = m ? inside(b, m->buf) : NOWHERE;
    s->off = 0;
    s->size = 0;
    if (off != NOWHERE && m->len <= 0xFFFFFFFFu) {
        s->off = (kvp_off)off;
        s->size = (uint32_t)m->len;
    }
}

int kvp_span_message(const kvp_buffer *b, const kvp_span *s, kvp_message *m)
{
    size_t max = 0;
    const uint8_t *p;
    if (!s || !m || !s->size)
        return KVP_E_ARGUMENT;
    p = kvp_buffer_getptr_at(b, s->off, &max);
    if (!p || max < s->size)
        return KVP_E_ARGUMENT;
    return kvp_header(p, s->size, m);
}

int kvp_tupleset_open(const kvp_buffer *b, const kvp_span *msg, uint16_t key, kvp_message *m, kvp_tupledef *def)
{
    int rc = kvp_span_message(b, msg, m);
    if (rc != KVP_OK)
        return rc;
    return kvp_get_tupledef(m, key, def);
}

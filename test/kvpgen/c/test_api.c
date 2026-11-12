/*
 * test_api.c - exercises the C code kvpack generates from test/kvpgen/api.kvp:
 * the canonical replies shared with the Go and Harbour tests, built in an
 * arena, written and read back, plus missing fields, nulls and error replies.
 */
#define _CRT_SECURE_NO_WARNINGS     /* sprintf, for MSVC */

#include "api_kvp.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

#define CHECK(cond) \
    do { if (!(cond)) { failures++; printf("%s:%d: FAILED: %s\n", __FILE__, __LINE__, #cond); } } while (0)

/* text_is says whether a field holds that text, in the buffer it refers to. */
static int text_is(const kvp_buffer *b, const kvp_text_st *t, const char *s)
{
    const uint8_t *p = kvp_st_ptr(b, t);
    size_t n = strlen(s);
    return !t->null && t->size == n && (n == 0 || (p && memcmp(p, s, n) == 0));
}

static int utf8_is(const kvp_utf8 *t, const char *s)
{
    return t->len == strlen(s) && memcmp(t->p, s, t->len) == 0;
}

/* The canonical reply (see canonicalReplyHex in test/kvpgen/kvpgen_test.go). */
/* In pieces: C89 caps a string literal at 509 characters. */
static const char *const canonical_hex[] = {
    "4b5650010100000005000000400100000c0f37fa0100040003000000616e6e020003000800000000f153650000000005",
    "0006001e0000004b56500165000000010000000a000000e6db35f00100040002000000646904000600670000004b5650",
    "0164000000050000005300000022dac938010001000400000007000000020004000100000061070006001e0000004b56",
    "500165000000010000000a00000055d90c4f0100040002000000626f0600010004000000010000000600010004000000",
    "0200000004000600880000004b565001640000000700000074000000411e320d01000100040000000800000002000400",
    "010000006203000200080000000500000000000000070006001e0000004b56500165000000010000000a000000455dc3",
    "a201000400020000006379080007000100000001090008000800000000000000000004400a0009000800000032303236",
    "31313131",
    NULL
};

static int nibble(char c)
{
    return c <= '9' ? c - '0' : c - 'a' + 10;
}

static size_t unhex(const char *const *parts, uint8_t *out)
{
    size_t n = 0;
    const char *s;
    for (; *parts; parts++)
        for (s = *parts; s[0] && s[1]; s += 2)
            out[n++] = (uint8_t)(nibble(s[0]) << 4 | nibble(s[1]));
    return n;
}

static void test_write_canonical(void)
{
    uint8_t want[512], *buf;
    size_t nwant = unhex(canonical_hex, want), len = 0, n;
    kvp_buffer a;
    api_login_reply r;
    api_event ev;

    /* everything of variable size goes into the arena; the structs are ours */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    memset(&r, 0, sizeof r);
    CHECK(kvp_set_text(&a, &r.name, "ann") == KVP_OK);
    r.expires.value = 1700000000;
    kvp_set_text(&a, &r.owner.value.name, "di");
    r.owner.has = 1;

    /* a record element is a struct of its own, its lists filled before it is added */
    memset(&ev, 0, sizeof ev);
    ev.id.value = 7;
    kvp_set_text(&a, &ev.title, "a");
    kvp_set_text(&a, &ev.who.value.name, "bo");
    CHECK(api_event_add_codes(&a, &ev, 1) == KVP_OK);
    CHECK(api_event_add_codes(&a, &ev, 2) == KVP_OK);
    CHECK(ev.codes.count == 2);
    CHECK(api_login_reply_add_events(&a, &r, &ev) == KVP_OK);

    memset(&ev, 0, sizeof ev);
    ev.id.value = 8;
    kvp_set_text(&a, &ev.title, "b");
    ev.big.value = 5;
    kvp_set_text(&a, &ev.who.value.name, "cy");
    ev.active.value = 1;
    ev.score.value = 2.5;
    ev.day.value = 20261111;
    CHECK(api_login_reply_add_events(&a, &r, &ev) == KVP_OK);
    CHECK(r.events.count == 2);

    /* the size is known before the message is written: the buffer is exact */
    n = api_login_reply_size(&r, &a);
    CHECK(n == nwant);
    buf = (uint8_t *)malloc(n);
    CHECK(buf != NULL);
    if (buf) {
        CHECK(api_login_reply_encode(&r, &a, buf, n, &len) == KVP_OK);
        CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
        CHECK(api_login_reply_encode(&r, &a, buf, n - 1, &len) == KVP_E_FULL);
        free(buf);
    }
    kvp_buffer_free(&a);
}

static void test_read_canonical(void)
{
    uint8_t bytes[512];
    size_t n = unhex(canonical_hex, bytes);
    kvp_buffer b;
    api_login_reply r;
    api_event ev;
    kvp_item it;
    int32_t code;
    int count = 0;

    /* the bytes received are the buffer: nothing is copied */
    kvp_buffer_wrap(&b, bytes, n);
    CHECK(api_login_reply_decode(&b, &r) == KVP_OK);
    CHECK(text_is(&b, &r.name, "ann"));
    CHECK(kvp_st_ptr(&b, &r.name) >= bytes && kvp_st_ptr(&b, &r.name) < bytes + n);
    CHECK(r.expires.value == 1700000000);
    CHECK(r.level.value == 0 && r.level.null == 0);
    CHECK(r.owner.has == 1 && r.owner.null == 0 && text_is(&b, &r.owner.value.name, "di"));
    CHECK(r.events.count == 2);

    memset(&it, 0, sizeof it);
    while (api_login_reply_events(&b, &r, &it, &ev) == KVP_OK) {
        count++;
        if (count == 1) {
            kvp_item ci;
            int ncodes = 0;
            CHECK(ev.id.value == 7 && text_is(&b, &ev.title, "a") && ev.big.value == 0);
            CHECK(text_is(&b, &ev.who.value.name, "bo"));
            CHECK(ev.blob.size == 0 && kvp_st_ptr(&b, &ev.blob) == NULL && ev.raw.size == 0);
            CHECK(ev.active.value == 0 && ev.score.value == 0 && ev.day.value == 0);
            CHECK(ev.codes.count == 2);
            memset(&ci, 0, sizeof ci);
            while (api_event_codes(&b, &ev, &ci, &code) == KVP_OK)
                CHECK(code == ++ncodes);
            CHECK(ncodes == 2);
        } else {
            CHECK(ev.id.value == 8 && text_is(&b, &ev.title, "b") && ev.big.value == 5);
            CHECK(text_is(&b, &ev.who.value.name, "cy"));
            CHECK(ev.active.value == 1 && ev.score.value == 2.5 && ev.day.value == 20261111);
            CHECK(ev.codes.count == 0);
        }
    }
    CHECK(count == 2);
}

/* The canonical query reply (see canonicalQueryHex in kvpgen_test.go): a
 * nullable field sent as a null, an optional nullable one that is null and
 * so left out, a record and a tupleset (id int32, name text) with the
 * tuples 1, "Ana" and 2, null. */
static const char *const query_hex[] = {
    "4b5650010300000006000000930000000704f72e01000100040000000700000002000a0000000000040006001e0000004b56",
    "500165000000010000000a000000e6db35f00100040002000000646905000b001e0000000200000001000200000069640000",
    "00000400040000006e616d650000000005000c001300000001000400000001000000040003000000416e6105000c00100000",
    "00010004000000020000000a0000000000",
    NULL
};

static void test_query(void)
{
    uint8_t want[256], buf[256];
    size_t nwant = unhex(query_hex, want), len = 0;
    kvp_buffer a, b;
    kvp_writer w, wdef;
    kvp_message m;
    kvp_tupledef def;
    api_query_reply r;
    kvp_field fd;
    kvp_item it, f;
    kvp_utf8 t;
    int32_t id = 0;
    int n = 0;

    /* build: the fields, and the tupleset straight on its member */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    memset(&r, 0, sizeof r);
    r.code.value = 7;
    kvp_set_text(&a, &r.note, "ignored");
    r.note.null = 1;                            /* null wins over the value */
    r.limit.value = 5;
    r.limit.null = 1;                           /* optional nullable: a null is left out */
    kvp_set_text(&a, &r.author.value.name, "di");
    CHECK(kvp_tupleset_field(&a, &r.rows, KVP_INT32, "id", NULL, 0) == KVP_OK);
    CHECK(kvp_tupleset_field(&a, &r.rows, KVP_TEXT, "name", NULL, 0) == KVP_OK);
    CHECK(kvp_tupleset_begin(&a, &r.rows) == KVP_OK);
    kvp_tupleset_int32(&a, &r.rows, 1);
    kvp_tupleset_text(&a, &r.rows, "Ana");
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_OK);
    CHECK(kvp_tupleset_begin(&a, &r.rows) == KVP_OK);
    kvp_tupleset_int32(&a, &r.rows, 2);
    kvp_tupleset_null(&a, &r.rows);
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_OK);
    CHECK(r.rows.nfields == 2 && r.rows.ntuples == 2);
    CHECK(api_query_reply_size(&r, &a) == nwant);
    CHECK(api_query_reply_encode(&r, &a, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);

    /* the builder refuses what a reader would */
    CHECK(kvp_tupleset_field(&a, &r.rows, KVP_INT32, "late", NULL, 0) == KVP_E_FORMAT);  /* after a tuple */
    CHECK(kvp_tupleset_field(&a, &r.extra, KVP_TUPLE, "t", NULL, 0) == KVP_E_TYPE);
    CHECK(kvp_tupleset_field(&a, &r.extra, KVP_INT32, "", NULL, 0) == KVP_E_ARGUMENT);
    CHECK(kvp_tupleset_begin(&a, &r.extra) == KVP_OK);                                   /* no fields: a tuple of no values */
    CHECK(kvp_tupleset_begin(&a, &r.extra) == KVP_E_FORMAT);                             /* one is open already */
    CHECK(kvp_tupleset_null(&a, &r.extra) == KVP_E_FORMAT);                              /* it takes no value */
    CHECK(kvp_tupleset_end(&a, &r.extra) == KVP_E_FORMAT);
    memset(&r.extra, 0, sizeof r.extra);
    CHECK(kvp_tupleset_int32(&a, &r.rows, 1) == KVP_E_FORMAT);                           /* no tuple open */
    kvp_tupleset_begin(&a, &r.rows);
    CHECK(kvp_tupleset_text(&a, &r.rows, "x") == KVP_E_TYPE);                            /* a text for the int32 */
    CHECK(kvp_tupleset_int32(&a, &r.rows, 3) == KVP_E_TYPE);                             /* it stays failed */
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_E_TYPE);
    kvp_tupleset_begin(&a, &r.rows);
    kvp_tupleset_int32(&a, &r.rows, 3);
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_E_FORMAT);                                /* one value short */
    kvp_tupleset_begin(&a, &r.rows);
    kvp_tupleset_int32(&a, &r.rows, 3);
    kvp_tupleset_null(&a, &r.rows);
    CHECK(kvp_tupleset_null(&a, &r.rows) == KVP_E_FORMAT);                               /* one too many */
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_E_FORMAT);
    kvp_tupleset_begin(&a, &r.rows);
    kvp_tupleset_int32(&a, &r.rows, 3);
    kvp_set_text(&a, &r.note, "in between");                                             /* something else got in */
    r.note.null = 1;
    CHECK(kvp_tupleset_null(&a, &r.rows) == KVP_E_FORMAT);
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_E_FORMAT);
    CHECK(kvp_tupleset_field(&a, &r.extra, KVP_INT32, "n", NULL, 0) == KVP_OK);
    CHECK(kvp_tupleset_field(&a, &r.extra, KVP_TEXT, "n", NULL, 0) == KVP_E_ARGUMENT);   /* a repeated name */
    memset(&r.extra, 0, sizeof r.extra);
    /* the tuples that failed were dropped: the message is the same */
    CHECK(r.rows.ntuples == 2);
    CHECK(api_query_reply_encode(&r, &a, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
    kvp_buffer_free(&a);

    /* read */
    kvp_buffer_wrap(&b, want, nwant);
    CHECK(api_query_reply_decode(&b, &r) == KVP_OK);
    CHECK(r.code.value == 7);
    CHECK(r.note.null == 1 && r.note.size == 0 && kvp_st_ptr(&b, &r.note) == NULL);
    CHECK(r.limit.null == 1 && r.limit.value == 0);     /* it did not arrive: null */
    CHECK(r.author.null == 0 && r.author.has == 1 && text_is(&b, &r.author.value.name, "di"));
    CHECK(r.rows.nfields == 2 && r.rows.ntuples == 2 && r.extra.nfields == 0);
    CHECK(kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK);
    CHECK(kvp_tupledef_field(&def, 1, &fd) == KVP_OK && fd.type == KVP_TEXT && utf8_is(&fd.name, "name"));
    for (it = def.item; kvp_tuple_next(&m, &it) == KVP_OK; ) {
        n++;
        CHECK(kvp_tuple_field(&def, &it, 0, &f) == KVP_OK && kvp_int32(&f, &id) == KVP_OK && id == n);
        CHECK(kvp_tuple_field(&def, &it, 1, &f) == KVP_OK);
        if (n == 1) {
            CHECK(kvp_item_utf8(&f, &t) == KVP_OK && utf8_is(&t, "Ana"));
        } else {
            CHECK(f.type == KVP_NULL);
        }
    }
    CHECK(n == 2);
    CHECK(kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_EXTRA, &m, &def) == KVP_E_NOT_FOUND);

    /* nulls where they may come; rows with no tuples; the optional tupleset */
    kvp_begin(&w, buf, sizeof buf, API_CMD_QUERY);
    kvp_add_int32(&w, API_QUERY_REPLY_CODE, 0);
    kvp_add_null(&w, API_QUERY_REPLY_NOTE);
    kvp_add_null(&w, API_QUERY_REPLY_LIMIT);
    kvp_add_null(&w, API_QUERY_REPLY_AUTHOR);
    kvp_begin_tupledef(&w, API_QUERY_REPLY_ROWS, &wdef);
    kvp_tupledef_add(&wdef, KVP_INT32, "n", NULL, 0);
    kvp_end_tupledef(&w, &wdef);
    kvp_begin_tupledef(&w, API_QUERY_REPLY_EXTRA, &wdef);
    kvp_tupledef_add(&wdef, KVP_TEXT, "x", NULL, 0);
    kvp_end_tupledef(&w, &wdef);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_query_reply_decode(&b, &r) == KVP_OK);
    CHECK(r.note.null == 1 && r.limit.null == 1 && r.author.null == 1 && r.author.has == 0);
    CHECK(r.rows.nfields == 1 && r.rows.ntuples == 0 && r.extra.nfields == 1);
    CHECK(kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK);
    it = def.item;
    CHECK(kvp_tuple_next(&m, &it) == KVP_E_NOT_FOUND);

    /* an optional nullable that arrives, even as a zero, is not null */
    kvp_begin(&w, buf, sizeof buf, API_CMD_QUERY);
    kvp_add_int32(&w, API_QUERY_REPLY_CODE, 0);
    kvp_add_text(&w, API_QUERY_REPLY_NOTE, "");
    kvp_add_int32(&w, API_QUERY_REPLY_LIMIT, 0);
    kvp_add_null(&w, API_QUERY_REPLY_AUTHOR);
    kvp_begin_tupledef(&w, API_QUERY_REPLY_ROWS, &wdef);
    kvp_tupledef_add(&wdef, KVP_INT32, "n", NULL, 0);
    kvp_end_tupledef(&w, &wdef);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_query_reply_decode(&b, &r) == KVP_OK);
    CHECK(r.note.null == 0 && r.note.size == 0 && r.limit.null == 0 && r.limit.value == 0);

    /* a struct at zero: nothing null, so the nullable ones go as empty
     * values -- the optional nullable too -- and the tupleset as a
     * definition of no fields; the optional tupleset is left out */
    memset(&r, 0, sizeof r);
    CHECK(api_query_reply_encode(&r, NULL, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == api_query_reply_size(&r, NULL));
    CHECK(kvp_parse(buf, len, &m) == KVP_OK && m.nitems == 5);
    CHECK(kvp_get_int32(&m, API_QUERY_REPLY_LIMIT, &id) == KVP_OK && id == 0);
    CHECK(kvp_get_utf8(&m, API_QUERY_REPLY_NOTE, &t) == KVP_OK && t.len == 0);
    CHECK(kvp_get_tupledef(&m, API_QUERY_REPLY_ROWS, &def) == KVP_OK && def.nfields == 0);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_query_reply_decode(&b, &r) == KVP_OK && r.rows.nfields == 0 && r.extra.nfields == 0);

    /* and with every null set: two nulls, and the optional one left out */
    memset(&r, 0, sizeof r);
    r.note.null = 1;
    r.limit.null = 1;
    r.author.null = 1;
    CHECK(api_query_reply_encode(&r, NULL, buf, sizeof buf, &len) == KVP_OK);
    CHECK(kvp_parse(buf, len, &m) == KVP_OK && m.nitems == 4);
    CHECK(kvp_is_null(&m, API_QUERY_REPLY_NOTE) == 1 && kvp_is_null(&m, API_QUERY_REPLY_AUTHOR) == 1);
    CHECK(kvp_find(&m, API_QUERY_REPLY_LIMIT, &it) == KVP_E_NOT_FOUND);

    /* what is not nullable refuses a null */
    kvp_begin(&w, buf, sizeof buf, API_CMD_QUERY);
    kvp_add_null(&w, API_QUERY_REPLY_CODE);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_query_reply_decode(&b, &r) == KVP_E_MISSING);
}

/* A tupleset of no fields is the empty one, and still takes tuples: each
 * of no values. Go and Harbour write and read the same. */
static void test_no_fields(void)
{
    uint8_t buf[256];
    size_t len = 0;
    kvp_buffer a, b;
    kvp_message m;
    kvp_tupledef def;
    api_query_reply r;
    kvp_item it;
    int n = 0;

    kvp_buffer_init(&a, malloc, free, 0, 0);
    memset(&r, 0, sizeof r);
    CHECK(kvp_tupleset_begin(&a, &r.rows) == KVP_OK);
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_OK);
    CHECK(kvp_tupleset_begin(&a, &r.rows) == KVP_OK);
    CHECK(kvp_tupleset_end(&a, &r.rows) == KVP_OK);
    CHECK(r.rows.nfields == 0 && r.rows.ntuples == 2);
    CHECK(api_query_reply_encode(&r, &a, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == api_query_reply_size(&r, &a));
    kvp_buffer_free(&a);

    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_query_reply_decode(&b, &r) == KVP_OK);
    CHECK(r.rows.nfields == 0 && r.rows.ntuples == 2);
    CHECK(kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK && def.nfields == 0);
    for (it = def.item; kvp_tuple_next(&m, &it) == KVP_OK; n++)
        CHECK(it.size == 0);
    CHECK(n == 2);
}

/* A result too big to guess: the arena grows (and moves) many times while
 * the tuples are added, and every offset still holds. */
static void test_big(void)
{
    kvp_buffer a, b;
    kvp_message m;
    kvp_tupledef def;
    api_query_reply r;
    kvp_field fd;
    kvp_item it, f;
    kvp_utf8 t;
    uint8_t *buf;
    char name[32];
    size_t n, len = 0;
    int32_t id = 0;
    int i, bad = 0, count = 0;

    kvp_buffer_init(&a, malloc, free, 0, 0);
    memset(&r, 0, sizeof r);
    r.code.value = 1;
    kvp_set_text(&a, &r.note, "a big one");
    kvp_set_text(&a, &r.author.value.name, "di");
    kvp_tupleset_field(&a, &r.rows, KVP_INT32, "id", NULL, 0);
    kvp_tupleset_field(&a, &r.rows, KVP_TEXT, "name", "meta", KVP_STRLEN);     /* a meta that is a string */
    kvp_tupleset_field(&a, &r.rows, KVP_FLOAT64, "x", "\x00\x01raw", 5);       /* and one that is not */
    for (i = 1; i <= 20000; i++) {
        sprintf(name, "name number %d", i);
        kvp_tupleset_begin(&a, &r.rows);
        kvp_tupleset_int32(&a, &r.rows, i);
        if (i % 7 == 0)
            kvp_tupleset_null(&a, &r.rows);
        else
            kvp_tupleset_text(&a, &r.rows, name);
        kvp_tupleset_float64(&a, &r.rows, i / 2.0);
        bad += kvp_tupleset_end(&a, &r.rows) != KVP_OK;
    }
    CHECK(bad == 0 && r.rows.ntuples == 20000);
    CHECK(text_is(&a, &r.note, "a big one"));           /* set before the arena moved */

    n = api_query_reply_size(&r, &a);
    buf = (uint8_t *)malloc(n);
    CHECK(buf != NULL);
    if (buf) {
        CHECK(api_query_reply_encode(&r, &a, buf, n, &len) == KVP_OK && len == n);
        kvp_buffer_free(&a);                            /* the message no longer needs it */

        kvp_buffer_wrap(&b, buf, len);
        CHECK(api_query_reply_decode(&b, &r) == KVP_OK);
        CHECK(r.rows.nfields == 3 && r.rows.ntuples == 20000 && text_is(&b, &r.note, "a big one"));
        CHECK(kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK);
        CHECK(kvp_tupledef_field(&def, 0, &fd) == KVP_OK && fd.meta.len == 0);
        CHECK(kvp_tupledef_field(&def, 1, &fd) == KVP_OK && fd.meta.len == 4 && memcmp(fd.meta.p, "meta", 4) == 0);
        CHECK(kvp_tupledef_field(&def, 2, &fd) == KVP_OK && fd.meta.len == 5 && memcmp(fd.meta.p, "\x00\x01raw", 5) == 0);
        for (it = def.item; kvp_tuple_next(&m, &it) == KVP_OK; ) {
            count++;
            sprintf(name, "name number %d", count);
            bad += kvp_tuple_field(&def, &it, 0, &f) != KVP_OK || kvp_int32(&f, &id) != KVP_OK || id != count;
            bad += kvp_tuple_field(&def, &it, 1, &f) != KVP_OK;
            if (count % 7 == 0)
                bad += f.type != KVP_NULL;
            else
                bad += kvp_item_utf8(&f, &t) != KVP_OK || !utf8_is(&t, name);
        }
        CHECK(count == 20000 && bad == 0);
        free(buf);
    } else {
        kvp_buffer_free(&a);
    }
}

static void *no_memory(size_t n)
{
    (void)n;
    return NULL;
}

static void test_errors(void)
{
    uint8_t buf[256], again[256];
    size_t len, len2 = 0;
    kvp_buffer a, b;
    kvp_writer w;
    kvp_message m;
    api_login l;
    api_login_reply r;
    kvp_text_st tag;
    kvp_utf8 t;
    kvp_item it;
    char text[32];
    int n = 0;

    /* password missing */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, API_LOGIN_USER, "bob");
    kvp_end(&w, &len);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_login_decode(&b, &l) == KVP_E_MISSING);

    /* password of another type */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, API_LOGIN_USER, "bob");
    kvp_add_int32(&w, API_LOGIN_PASSWORD, 5);
    kvp_end(&w, &len);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_login_decode(&b, &l) == KVP_E_MISSING);

    /* not a message, and no buffer */
    kvp_buffer_wrap(&b, "hello", 5);
    CHECK(api_login_decode(&b, &l) == KVP_E_TRUNCATED);
    CHECK(api_login_decode(NULL, &l) == KVP_E_ARGUMENT);

    /* optional left out, a list of texts, and the walk of what was decoded */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    memset(&l, 0, sizeof l);
    kvp_set_text(&a, &l.user, "bob");
    kvp_set_text(&a, &l.password, "x");
    CHECK(api_login_size(&l, &a) == KVP_HEADER_SIZE + 2 * KVP_ITEM_SIZE + 4);
    t = kvp_utf8_of("one");
    CHECK(api_login_add_tags(&a, &l, &t) == KVP_OK);
    t = kvp_utf8_of("");
    CHECK(api_login_add_tags(&a, &l, &t) == KVP_OK);
    t = kvp_st_utf8(&a, &l.user);                       /* bytes that lie in the arena itself */
    CHECK(api_login_add_tags(&a, &l, &t) == KVP_OK);
    CHECK(l.tags.count == 3);
    CHECK(api_login_encode(&l, &a, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == api_login_size(&l, &a));
    CHECK(api_login_encode(&l, &a, again, 30, &len2) == KVP_E_FULL);   /* a buffer too small */
    kvp_buffer_free(&a);

    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_login_decode(&b, &l) == KVP_OK && l.when.value == 0 && text_is(&b, &l.user, "bob"));
    CHECK(l.tags.count == 3);
    memset(&it, 0, sizeof it);
    while (api_login_tags(&b, &l, &it, &tag) == KVP_OK) {
        n++;
        CHECK(text_is(&b, &tag, n == 1 ? "one" : n == 2 ? "" : "bob"));
    }
    CHECK(n == 3);
    /* a decoded struct is written again from its buffer, its lists as they arrived */
    CHECK(api_login_encode(&l, &b, again, sizeof again, &len2) == KVP_OK);
    CHECK(len2 == len && memcmp(again, buf, len) == 0 && kvp_parse(again, len2, &m) == KVP_OK);
    CHECK(kvp_get_utf8(&m, API_LOGIN_USER, &t) == KVP_OK && utf8_is(&t, "bob"));

    /* error reply */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, KVP_KEY_ERROR, "wrong password");
    kvp_end(&w, &len);
    kvp_buffer_wrap(&b, buf, len);
    CHECK(api_login_reply_decode(&b, &r) == KVP_E_REPLY);
    CHECK(kvp_span_message(&b, &r.kvp, &m) == KVP_OK);
    CHECK(kvp_error_text(&m, text, sizeof text) == KVP_OK && strcmp(text, "wrong password") == 0);

    /* no memory: nothing is set, and a struct that refers nowhere is not written */
    kvp_buffer_init(&a, no_memory, free, 0, 0);
    memset(&l, 0, sizeof l);
    CHECK(kvp_set_text(&a, &l.user, "bob") == KVP_E_FULL && l.user.size == 0);
    t = kvp_utf8_of("one");
    CHECK(api_login_add_tags(&a, &l, &t) == KVP_E_FULL && l.tags.count == 0);
    l.user.size = 3;
    l.user.off = 77;
    CHECK(api_login_encode(&l, &a, buf, sizeof buf, &len) == KVP_E_ARGUMENT);
    /* received bytes are not an arena: they do not grow */
    kvp_buffer_wrap(&b, buf, sizeof buf);
    memset(&l, 0, sizeof l);
    CHECK(kvp_set_text(&b, &l.user, "bob") == KVP_E_FULL);
}

/* --- command every ----------------------------------------------------------
 * Each type in each form a field can take: required (r_), optional (o_),
 * nullable (n_), optional nullable (on_) and list (l_), plus a required and
 * an optional tupleset. Three messages of it, the same in the Go test
 * (everyFullHex, everyNullsHex, everyZerosHex in kvpgen_test.go) and in the
 * Harbour one:
 *   full   every field with a value, two elements per list, a tupleset with
 *          a field of each type, one tuple with values and one all null
 *   zeros  what a struct nobody touched writes, the same in the three
 *          languages: every field that always travels as its empty value (a
 *          zero, no bytes, a message of command 0 and no items, a record of
 *          empty fields, a tuple definition of no fields), nothing of the
 *          optional fields nor the lists
 *   nulls  the same with every field that can be null set to null: the
 *          nullable ones go as nulls, the optional nullable ones are left out
 */
static const char *const every_full_hex[] = {
    "4b56500104000000400000003b0500007860084f0100010004000000ffffffff020002000800000000000000000100000300",
    "03000800000000f153650000000004000400040000007465787405000500030000000001ff06000700010000000107000800",
    "0800000000000000000004400800090008000000323032363131313209000600140000004b56500109000000000000000000",
    "0000000000000a0006001d0000004b5650016500000001000000090000005e1d56860100040001000000720b000100040000",
    "000b0000000c000200080000000c000000000000000d000300080000000df15365000000000e000400010000006f0f000500",
    "010000000f100007000100000001110008000800000000000000008031401200090008000000323032363031313813000600",
    "140000004b56500113000000000000000000000000000000140006001d0000004b5650016500000001000000090000008771",
    "50e501000400010000006f15000100040000001500000016000200080000001600000000000000170003000800000017f153",
    "650000000018000400010000006e1900050001000000191a00070001000000011b000800080000000000000000803b401c00",
    "09000800000032303236303132381d000600140000004b5650011d0000000000000000000000000000001e0006001d000000",
    "4b5650016500000001000000090000001141579201000400010000006e1f000100040000001f000000200002000800000020",
    "00000000000000210003000800000021f153650000000022000400020000006f6e2300050001000000232400070001000000",
    "0125000800080000000000000000c042402600090008000000323032363032313827000600140000004b5650012700000000",
    "0000000000000000000000280006001e0000004b56500165000000010000000a0000008e97a58d01000400020000006f6e29",
    "00010004000000010000002900010004000000feffffff2a0002000800000003000000000000002a00020008000000fcffff",
    "ffffffffff2b000300080000002bf15365000000002b0003000800000000000000000000002c00040001000000612c000400",
    "000000002d0005000200000001022d000500000000002e00070001000000012e00070001000000002f000800080000000000",
    "00000000e03f2f00080008000000000000000000f8bf30000900080000003230323630313031300009000800000020202020",
    "2020202031000600140000004b5650010100000000000000000000000000000031000600140000004b565001020000000000",
    "00000000000000000000320006001d0000004b56500165000000010000000900000040f48366010004000100000078320006",
    "001d0000004b565001650000000100000009000000d6c4841101000400010000007933000b006d0000000900000001000100",
    "000069000000000200010000006a00000000030001000000740000000004000100000073060000006c656e3d343005000100",
    "000062000000000600010000006d000000000700010000006b00000000080001000000660000000009000100000064000000",
    "0033000c007300000001000400000001000000020008000000020000000000000003000800000003f1536500000000040003",
    "000000416e61050001000000040600140000004b565001050000000000000000000000000000000700010000000108000800",
    "00000000000000001a40090008000000323032363037303733000c00360000000a00000000000a00000000000a0000000000",
    "0a00000000000a00000000000a00000000000a00000000000a00000000000a000000000034000b000f000000010000000100",
    "010000006e00000000",
    NULL
};

static const char *const every_nulls_hex[] = {
    "4b565001040000001500000001010000ecb25ee1010001000400000000000000020002000800000000000000000000000300",
    "0300080000000000000000000000040004000000000005000500000000000600070001000000000700080008000000000000",
    "00000000000800090008000000202020202020202009000600140000004b565001000000000000000000000000000000000a",
    "0006001c0000004b565001650000000100000008000000e19d1932010004000000000015000a000000000016000a00000000",
    "0017000a000000000018000a000000000019000a00000000001a000a00000000001b000a00000000001c000a00000000001d",
    "000a00000000001e000a000000000033000b000400000000000000",
    NULL
};

static const char *const every_zeros_hex[] = {
    "4b565001040000001f000000fb010000d3e7d1a4010001000400000000000000020002000800000000000000000000000300",
    "0300080000000000000000000000040004000000000005000500000000000600070001000000000700080008000000000000",
    "00000000000800090008000000202020202020202009000600140000004b565001000000000000000000000000000000000a",
    "0006001c0000004b565001650000000100000008000000e19d19320100040000000000150001000400000000000000160002",
    "0008000000000000000000000017000300080000000000000000000000180004000000000019000500000000001a00070001",
    "000000001b0008000800000000000000000000001c0009000800000020202020202020201d000600140000004b5650010000",
    "00000000000000000000000000001e0006001c0000004b565001650000000100000008000000e19d19320100040000000000",
    "1f00010004000000000000002000020008000000000000000000000021000300080000000000000000000000220004000000",
    "0000230005000000000024000700010000000025000800080000000000000000000000260009000800000020202020202020",
    "2027000600140000004b56500100000000000000000000000000000000280006001c0000004b565001650000000100000008",
    "000000e19d1932010004000000000033000b000400000000000000",
    NULL
};

#define OK(call) CHECK((call) == KVP_OK)

/* empty_msg writes a message with no items, of that command, into the
 * KVP_HEADER_SIZE bytes of p. */
static void empty_msg(uint8_t *p, uint32_t command)
{
    kvp_writer w;
    size_t len = 0;
    kvp_begin(&w, p, KVP_HEADER_SIZE, command);
    kvp_end(&w, &len);
}

static int set_msg(kvp_buffer *a, kvp_msg_st *f, uint32_t command)
{
    uint8_t p[KVP_HEADER_SIZE];
    empty_msg(p, command);
    return kvp_set_bytes(a, f, p, KVP_HEADER_SIZE);
}

/* msg_command is the command of the message a msg field holds, -1 for none. */
static long msg_command(const kvp_buffer *b, const kvp_msg_st *f)
{
    kvp_message m;
    return kvp_st_msg(b, f, &m) == KVP_OK ? (long)m.command : -1;
}

static int bin_is(const kvp_buffer *b, const kvp_bin_st *f, const uint8_t *bytes, uint32_t n)
{
    const uint8_t *p = kvp_st_ptr(b, f);
    return !f->null && f->size == n && (n == 0 || (p && memcmp(p, bytes, n) == 0));
}

static const uint8_t r_bin[] = { 0, 1, 255 }, o_bin[] = { 15 }, n_bin[] = { 25 }, on_bin[] = { 35 };
static const uint8_t l_bin[] = { 1, 2 }, t_bin[] = { 4 };

static void every_full(kvp_buffer *a, api_every *e)
{
    uint8_t sub[KVP_HEADER_SIZE];
    kvp_message m;
    kvp_utf8 t;
    kvp_bytes bytes;
    api_person p;
    int i;

    memset(e, 0, sizeof *e);

    /* required */
    e->r_int32.value = -1;
    e->r_int64.value = (int64_t)1 << 40;
    e->r_ts.value = 1700000000;
    OK(kvp_set_text(a, &e->r_text, "text"));
    OK(kvp_set_bytes(a, &e->r_bin, r_bin, 3));
    e->r_bool.value = 1;
    e->r_float64.value = 2.5;
    e->r_ds.value = 20261112;
    OK(set_msg(a, &e->r_msg, 9));
    OK(kvp_set_text(a, &e->r_rec.value.name, "r"));

    /* optional: an optional record is there when its has says so */
    e->o_int32.value = 11;
    e->o_int64.value = 12;
    e->o_ts.value = 1700000013;
    OK(kvp_set_text(a, &e->o_text, "o"));
    OK(kvp_set_bytes(a, &e->o_bin, o_bin, 1));
    e->o_bool.value = 1;
    e->o_float64.value = 17.5;
    e->o_ds.value = 20260118;
    OK(set_msg(a, &e->o_msg, 19));
    OK(kvp_set_text(a, &e->o_rec.value.name, "o"));
    e->o_rec.has = 1;

    /* nullable */
    e->n_int32.value = 21;
    e->n_int64.value = 22;
    e->n_ts.value = 1700000023;
    OK(kvp_set_text(a, &e->n_text, "n"));
    OK(kvp_set_bytes(a, &e->n_bin, n_bin, 1));
    e->n_bool.value = 1;
    e->n_float64.value = 27.5;
    e->n_ds.value = 20260128;
    OK(set_msg(a, &e->n_msg, 29));
    OK(kvp_set_text(a, &e->n_rec.value.name, "n"));

    /* optional nullable */
    e->on_int32.value = 31;
    e->on_int64.value = 32;
    e->on_ts.value = 1700000033;
    OK(kvp_set_text(a, &e->on_text, "on"));
    OK(kvp_set_bytes(a, &e->on_bin, on_bin, 1));
    e->on_bool.value = 1;
    e->on_float64.value = 37.5;
    e->on_ds.value = 20260218;
    OK(set_msg(a, &e->on_msg, 39));
    OK(kvp_set_text(a, &e->on_rec.value.name, "on"));

    /* lists: two elements each, the second one empty where the type has an empty */
    OK(api_every_add_l_int32(a, e, 1));
    OK(api_every_add_l_int32(a, e, -2));
    OK(api_every_add_l_int64(a, e, 3));
    OK(api_every_add_l_int64(a, e, -4));
    OK(api_every_add_l_ts(a, e, 1700000043));
    OK(api_every_add_l_ts(a, e, 0));
    t = kvp_utf8_of("a");
    OK(api_every_add_l_text(a, e, &t));
    t = kvp_utf8_of("");
    OK(api_every_add_l_text(a, e, &t));
    bytes.p = l_bin;
    bytes.len = 2;
    OK(api_every_add_l_bin(a, e, &bytes));
    bytes.len = 0;
    OK(api_every_add_l_bin(a, e, &bytes));
    OK(api_every_add_l_bool(a, e, 1));
    OK(api_every_add_l_bool(a, e, 0));
    OK(api_every_add_l_float64(a, e, 0.5));
    OK(api_every_add_l_float64(a, e, -1.5));
    OK(api_every_add_l_ds(a, e, 20260101));
    OK(api_every_add_l_ds(a, e, 0));
    for (i = 1; i <= 2; i++) {
        empty_msg(sub, (uint32_t)i);
        OK(kvp_parse(sub, sizeof sub, &m));
        OK(api_every_add_l_msg(a, e, &m));
    }
    memset(&p, 0, sizeof p);
    OK(kvp_set_text(a, &p.name, "x"));
    OK(api_every_add_l_rec(a, e, &p));
    memset(&p, 0, sizeof p);
    OK(kvp_set_text(a, &p.name, "y"));
    OK(api_every_add_l_rec(a, e, &p));

    /* a tupleset with a field of each type: a tuple with values, one all null */
    OK(kvp_tupleset_field(a, &e->r_set, KVP_INT32, "i", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_INT64, "j", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_TS, "t", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_TEXT, "s", "len=40", KVP_STRLEN));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_BIN, "b", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_MSG, "m", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_BOOL, "k", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_FLOAT64, "f", NULL, 0));
    OK(kvp_tupleset_field(a, &e->r_set, KVP_DS, "d", NULL, 0));
    empty_msg(sub, 5);
    OK(kvp_tupleset_begin(a, &e->r_set));
    OK(kvp_tupleset_int32(a, &e->r_set, 1));
    OK(kvp_tupleset_int64(a, &e->r_set, 2));
    OK(kvp_tupleset_ts(a, &e->r_set, 1700000003));
    OK(kvp_tupleset_text(a, &e->r_set, "Ana"));
    OK(kvp_tupleset_bin(a, &e->r_set, t_bin, 1));
    OK(kvp_tupleset_msg(a, &e->r_set, sub, sizeof sub));
    OK(kvp_tupleset_bool(a, &e->r_set, 1));
    OK(kvp_tupleset_float64(a, &e->r_set, 6.5));
    OK(kvp_tupleset_ds(a, &e->r_set, 20260707));
    OK(kvp_tupleset_end(a, &e->r_set));
    OK(kvp_tupleset_begin(a, &e->r_set));
    for (i = 0; i < 9; i++)
        OK(kvp_tupleset_null(a, &e->r_set));
    OK(kvp_tupleset_end(a, &e->r_set));

    OK(kvp_tupleset_field(a, &e->o_set, KVP_INT32, "n", NULL, 0));
}

/* every_zeros: a struct nobody touched. Nothing null, everything empty. */
static void every_zeros(api_every *e)
{
    memset(e, 0, sizeof *e);
}

/* every_nulls: every_zeros with every field that can be null set to null. */
static void every_nulls(api_every *e)
{
    every_zeros(e);
    e->n_int32.null = e->n_int64.null = e->n_ts.null = e->n_text.null = e->n_bin.null = 1;
    e->n_bool.null = e->n_float64.null = e->n_ds.null = e->n_msg.null = e->n_rec.null = 1;
    e->on_int32.null = e->on_int64.null = e->on_ts.null = e->on_text.null = e->on_bin.null = 1;
    e->on_bool.null = e->on_float64.null = e->on_ds.null = e->on_msg.null = e->on_rec.null = 1;
}

#define EVERY_MAX 2048      /* room for the biggest of the three messages */

/* keys_are says whether the message carries exactly those keys, in order,
 * a null given as the negative of its key. */
static int keys_are(const uint8_t *buf, size_t len, const int *want, int nwant)
{
    kvp_message m;
    kvp_item it;
    int rc, n = 0;
    if (kvp_parse(buf, len, &m) != KVP_OK)
        return 0;
    for (rc = kvp_first(&m, &it); rc == KVP_OK; rc = kvp_next(&m, &it), n++)
        if (n >= nwant || want[n] != (it.type == KVP_NULL ? -(int)it.key : (int)it.key))
            return 0;
    return n == nwant;
}

static int span(int *keys, int n, int from, int to, int sign)
{
    for (; from <= to; from++)
        keys[n++] = sign * from;
    return n;
}

/* The three messages are written as the Go and Harbour code writes them,
 * and each carries the keys the rule of its form says. */
static void test_every_write(void)
{
    static uint8_t want[EVERY_MAX], buf[EVERY_MAX];
    int keys[80], n, k;
    size_t nwant, len = 0;
    kvp_buffer a;
    api_every e;

    /* full: everything, two elements per list, a definition and two tuples, a definition */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    every_full(&a, &e);
    nwant = unhex(every_full_hex, want);
    CHECK(api_every_size(&e, &a) == nwant);
    OK(api_every_encode(&e, &a, buf, sizeof buf, &len));
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
    n = span(keys, 0, 1, 40, 1);
    for (k = 41; k <= 50; k++) {
        keys[n++] = k;
        keys[n++] = k;
    }
    keys[n++] = 51;
    keys[n++] = 51;
    keys[n++] = 51;
    keys[n++] = 52;
    CHECK(keys_are(buf, len, keys, n));
    kvp_buffer_free(&a);

    /* nulls: the required fields, the nullable ones as nulls, and nothing
     * of the optional, the optional nullable nor the lists */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    every_nulls(&e);
    nwant = unhex(every_nulls_hex, want);
    CHECK(api_every_size(&e, &a) == nwant);
    OK(api_every_encode(&e, &a, buf, sizeof buf, &len));
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
    n = span(keys, 0, 1, 10, 1);
    n = span(keys, n, 21, 30, -1);
    keys[n++] = 51;
    CHECK(keys_are(buf, len, keys, n));
    kvp_buffer_free(&a);

    /* zeros: a struct at zero needs no arena. An optional field at zero is
     * left out; a nullable one and an optional nullable one at zero are
     * values, and travel */
    every_zeros(&e);
    nwant = unhex(every_zeros_hex, want);
    CHECK(api_every_size(&e, NULL) == nwant);
    OK(api_every_encode(&e, NULL, buf, sizeof buf, &len));
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
    n = span(keys, 0, 1, 10, 1);
    n = span(keys, n, 21, 40, 1);
    keys[n++] = 51;
    CHECK(keys_are(buf, len, keys, n));

    /* the same message with the messages given by hand, as the empty one:
     * no bytes and the empty message are one thing */
    kvp_buffer_init(&a, malloc, free, 0, 0);
    every_zeros(&e);
    OK(set_msg(&a, &e.r_msg, 0));
    OK(set_msg(&a, &e.n_msg, 0));
    OK(set_msg(&a, &e.on_msg, 0));
    e.r_rec.has = e.n_rec.has = e.on_rec.has = 1;
    len = 0;
    OK(api_every_encode(&e, &a, buf, sizeof buf, &len));
    CHECK(len == nwant && memcmp(buf, want, nwant) == 0);
    kvp_buffer_free(&a);
}

/* The three messages are read and every field looked at. */
static void test_every_read(void)
{
    static uint8_t bytes[EVERY_MAX], again[EVERY_MAX];
    size_t n, len = 0;
    kvp_buffer b;
    kvp_message m;
    kvp_tupledef def;
    kvp_field fd;
    kvp_item it, f;
    kvp_utf8 t;
    kvp_text_st text;
    kvp_bin_st bin;
    kvp_msg_st msg;
    api_every e;
    api_person p;
    const void *ptr;
    uint32_t size;
    int32_t i32 = 0;
    int64_t i64 = 0;
    double d = 0;
    int flag = 0, count, i;

    /* full */
    n = unhex(every_full_hex, bytes);
    kvp_buffer_wrap(&b, bytes, n);
    OK(api_every_decode(&b, &e));

    CHECK(e.r_int32.value == -1 && e.r_int64.value == (int64_t)1 << 40 && e.r_ts.value == 1700000000);
    CHECK(text_is(&b, &e.r_text, "text") && bin_is(&b, &e.r_bin, r_bin, 3));
    CHECK(e.r_bool.value == 1 && e.r_float64.value == 2.5 && e.r_ds.value == 20261112);
    CHECK(msg_command(&b, &e.r_msg) == 9 && e.r_rec.has == 1 && text_is(&b, &e.r_rec.value.name, "r"));

    CHECK(e.o_int32.value == 11 && e.o_int64.value == 12 && e.o_ts.value == 1700000013);
    CHECK(text_is(&b, &e.o_text, "o") && bin_is(&b, &e.o_bin, o_bin, 1));
    CHECK(e.o_bool.value == 1 && e.o_float64.value == 17.5 && e.o_ds.value == 20260118);
    CHECK(msg_command(&b, &e.o_msg) == 19 && e.o_rec.has == 1 && text_is(&b, &e.o_rec.value.name, "o"));

    CHECK(e.n_int32.value == 21 && e.n_int64.value == 22 && e.n_ts.value == 1700000023);
    CHECK(text_is(&b, &e.n_text, "n") && bin_is(&b, &e.n_bin, n_bin, 1));
    CHECK(e.n_bool.value == 1 && e.n_float64.value == 27.5 && e.n_ds.value == 20260128);
    CHECK(msg_command(&b, &e.n_msg) == 29 && e.n_rec.has == 1 && text_is(&b, &e.n_rec.value.name, "n"));
    CHECK(!e.n_int32.null && !e.n_int64.null && !e.n_ts.null && !e.n_text.null && !e.n_bin.null);
    CHECK(!e.n_bool.null && !e.n_float64.null && !e.n_ds.null && !e.n_msg.null && !e.n_rec.null);

    CHECK(e.on_int32.value == 31 && e.on_int64.value == 32 && e.on_ts.value == 1700000033);
    CHECK(text_is(&b, &e.on_text, "on") && bin_is(&b, &e.on_bin, on_bin, 1));
    CHECK(e.on_bool.value == 1 && e.on_float64.value == 37.5 && e.on_ds.value == 20260218);
    CHECK(msg_command(&b, &e.on_msg) == 39 && e.on_rec.has == 1 && text_is(&b, &e.on_rec.value.name, "on"));
    CHECK(!e.on_int32.null && !e.on_int64.null && !e.on_ts.null && !e.on_text.null && !e.on_bin.null);
    CHECK(!e.on_bool.null && !e.on_float64.null && !e.on_ds.null && !e.on_msg.null && !e.on_rec.null);

    /* the lists: counted when decoded, walked over the message */
    CHECK(e.l_int32.count == 2 && e.l_int64.count == 2 && e.l_ts.count == 2 && e.l_text.count == 2);
    CHECK(e.l_bin.count == 2 && e.l_bool.count == 2 && e.l_float64.count == 2 && e.l_ds.count == 2);
    CHECK(e.l_msg.count == 2 && e.l_rec.count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_int32(&b, &e, &it, &i32) == KVP_OK; count++)
        CHECK(i32 == (count == 0 ? 1 : -2));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_int64(&b, &e, &it, &i64) == KVP_OK; count++)
        CHECK(i64 == (count == 0 ? 3 : -4));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_ts(&b, &e, &it, &i64) == KVP_OK; count++)
        CHECK(i64 == (count == 0 ? 1700000043 : 0));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_text(&b, &e, &it, &text) == KVP_OK; count++)
        CHECK(text_is(&b, &text, count == 0 ? "a" : ""));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_bin(&b, &e, &it, &bin) == KVP_OK; count++)
        CHECK(bin_is(&b, &bin, l_bin, count == 0 ? 2 : 0));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_bool(&b, &e, &it, &flag) == KVP_OK; count++)
        CHECK(flag == (count == 0));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_float64(&b, &e, &it, &d) == KVP_OK; count++)
        CHECK(d == (count == 0 ? 0.5 : -1.5));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_ds(&b, &e, &it, &i32) == KVP_OK; count++)
        CHECK(i32 == (count == 0 ? 20260101 : 0));
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_msg(&b, &e, &it, &msg) == KVP_OK; count++)
        CHECK(msg_command(&b, &msg) == count + 1);
    CHECK(count == 2);
    memset(&it, 0, sizeof it);
    for (count = 0; api_every_l_rec(&b, &e, &it, &p) == KVP_OK; count++)
        CHECK(text_is(&b, &p.name, count == 0 ? "x" : "y"));
    CHECK(count == 2);

    /* the tuplesets: a field of each type, a tuple with values and one all null */
    CHECK(e.r_set.nfields == 9 && e.r_set.ntuples == 2 && e.o_set.nfields == 1 && e.o_set.ntuples == 0);
    OK(kvp_tupleset_open(&b, &e.kvp, API_EVERY_R_SET, &m, &def));
    CHECK(kvp_tupledef_field(&def, 3, &fd) == KVP_OK && fd.type == KVP_TEXT && utf8_is(&fd.name, "s"));
    CHECK(fd.meta.len == 6 && memcmp(fd.meta.p, "len=40", 6) == 0);
    it = def.item;
    OK(kvp_tuple_next(&m, &it));
    CHECK(kvp_tuple_field(&def, &it, 0, &f) == KVP_OK && kvp_int32(&f, &i32) == KVP_OK && i32 == 1);
    CHECK(kvp_tuple_field(&def, &it, 1, &f) == KVP_OK && kvp_int64(&f, &i64) == KVP_OK && i64 == 2);
    CHECK(kvp_tuple_field(&def, &it, 2, &f) == KVP_OK && kvp_ts(&f, &i64) == KVP_OK && i64 == 1700000003);
    CHECK(kvp_tuple_field(&def, &it, 3, &f) == KVP_OK && kvp_item_utf8(&f, &t) == KVP_OK && utf8_is(&t, "Ana"));
    CHECK(kvp_tuple_field(&def, &it, 4, &f) == KVP_OK && kvp_bin(&f, &ptr, &size) == KVP_OK && size == 1 && *(const uint8_t *)ptr == 4);
    CHECK(kvp_tuple_field(&def, &it, 5, &f) == KVP_OK && kvp_msg(&f, &m) == KVP_OK && m.command == 5);
    OK(kvp_span_message(&b, &e.kvp, &m));
    CHECK(kvp_tuple_field(&def, &it, 6, &f) == KVP_OK && kvp_bool(&f, &flag) == KVP_OK && flag == 1);
    CHECK(kvp_tuple_field(&def, &it, 7, &f) == KVP_OK && kvp_float64(&f, &d) == KVP_OK && d == 6.5);
    CHECK(kvp_tuple_field(&def, &it, 8, &f) == KVP_OK && kvp_ds(&f, &i32) == KVP_OK && i32 == 20260707);
    OK(kvp_tuple_next(&m, &it));
    for (i = 0; i < 9; i++)
        CHECK(kvp_tuple_field(&def, &it, (uint32_t)i, &f) == KVP_OK && f.type == KVP_NULL && f.size == 0);
    CHECK(kvp_tuple_next(&m, &it) == KVP_E_NOT_FOUND);
    OK(kvp_tupleset_open(&b, &e.kvp, API_EVERY_O_SET, &m, &def));
    CHECK(def.nfields == 1);

    /* what was read writes the same bytes again, lists and tuplesets too,
     * with the buffer it was read from */
    CHECK(api_every_size(&e, &b) == n);
    OK(api_every_encode(&e, &b, again, sizeof again, &len));
    CHECK(len == n && memcmp(again, bytes, n) == 0);

    /* nulls: an optional field that did not arrive reads empty; a nullable
     * one that came as a null and an optional nullable one that did not
     * arrive read null */
    n = unhex(every_nulls_hex, bytes);
    kvp_buffer_wrap(&b, bytes, n);
    OK(api_every_decode(&b, &e));
    CHECK(e.r_int32.value == 0 && e.r_int64.value == 0 && e.r_ts.value == 0 && text_is(&b, &e.r_text, ""));
    CHECK(e.r_bin.size == 0 && e.r_bool.value == 0 && e.r_float64.value == 0 && e.r_ds.value == 0);
    CHECK(msg_command(&b, &e.r_msg) == 0 && e.r_rec.has == 1 && text_is(&b, &e.r_rec.value.name, ""));
    CHECK(e.o_int32.value == 0 && e.o_int64.value == 0 && e.o_ts.value == 0 && e.o_text.size == 0 && e.o_bin.size == 0);
    CHECK(e.o_bool.value == 0 && e.o_float64.value == 0 && e.o_ds.value == 0 && e.o_msg.size == 0 && e.o_rec.has == 0);
    CHECK(!e.o_int32.null && !e.o_text.null && !e.o_msg.null && !e.o_rec.null);
    CHECK(e.n_int32.null && e.n_int64.null && e.n_ts.null && e.n_text.null && e.n_bin.null);
    CHECK(e.n_bool.null && e.n_float64.null && e.n_ds.null && e.n_msg.null && e.n_rec.null && e.n_rec.has == 0);
    CHECK(e.on_int32.null && e.on_int64.null && e.on_ts.null && e.on_text.null && e.on_bin.null);
    CHECK(e.on_bool.null && e.on_float64.null && e.on_ds.null && e.on_msg.null && e.on_rec.null && e.on_rec.has == 0);
    CHECK(e.l_int32.count == 0 && e.l_int64.count == 0 && e.l_ts.count == 0 && e.l_text.count == 0 && e.l_bin.count == 0);
    CHECK(e.l_bool.count == 0 && e.l_float64.count == 0 && e.l_ds.count == 0 && e.l_msg.count == 0 && e.l_rec.count == 0);
    memset(&it, 0, sizeof it);
    CHECK(api_every_l_int32(&b, &e, &it, &i32) == KVP_E_NOT_FOUND);
    /* the tupleset of no fields arrived; the optional one did not, and reads the same */
    CHECK(e.r_set.nfields == 0 && e.r_set.ntuples == 0 && e.o_set.nfields == 0 && e.o_set.ntuples == 0);
    CHECK(kvp_tupleset_open(&b, &e.kvp, API_EVERY_R_SET, &m, &def) == KVP_OK && def.nfields == 0);
    CHECK(kvp_tupleset_open(&b, &e.kvp, API_EVERY_O_SET, &m, &def) == KVP_E_NOT_FOUND);
    OK(api_every_encode(&e, &b, again, sizeof again, &len));
    CHECK(len == n && memcmp(again, bytes, n) == 0);

    /* zeros: a zero that arrives is a value, not a null */
    n = unhex(every_zeros_hex, bytes);
    kvp_buffer_wrap(&b, bytes, n);
    OK(api_every_decode(&b, &e));
    CHECK(!e.n_int32.null && !e.n_int64.null && !e.n_ts.null && !e.n_text.null && !e.n_bin.null);
    CHECK(!e.n_bool.null && !e.n_float64.null && !e.n_ds.null && !e.n_msg.null && !e.n_rec.null && e.n_rec.has == 1);
    CHECK(!e.on_int32.null && !e.on_int64.null && !e.on_ts.null && !e.on_text.null && !e.on_bin.null);
    CHECK(!e.on_bool.null && !e.on_float64.null && !e.on_ds.null && !e.on_msg.null && !e.on_rec.null && e.on_rec.has == 1);
    CHECK(e.n_int32.value == 0 && e.n_text.size == 0 && e.n_ds.value == 0 && msg_command(&b, &e.n_msg) == 0);
    CHECK(e.on_int32.value == 0 && e.on_text.size == 0 && e.on_ds.value == 0 && msg_command(&b, &e.on_msg) == 0);
    OK(api_every_encode(&e, &b, again, sizeof again, &len));
    CHECK(len == n && memcmp(again, bytes, n) == 0);
}

/* How test_every_key_by_key changes one key of the full message. */
#define DROP_KEY  0     /* the key does not arrive */
#define NULL_KEY  1     /* it arrives as a null */
#define OTHER_KEY 2     /* it arrives with another type */

/* change copies a message with the items of key dropped, or the first of
 * them replaced by a null or by a value of another type. */
static size_t change(const uint8_t *src, size_t n, uint16_t key, int how, uint8_t *out, size_t cap)
{
    kvp_message m;
    kvp_writer w;
    kvp_item it;
    size_t len = 0;
    int rc, done = 0;

    if (kvp_parse(src, n, &m) != KVP_OK || kvp_begin(&w, out, cap, m.command) != KVP_OK)
        return 0;
    for (rc = kvp_first(&m, &it); rc == KVP_OK; rc = kvp_next(&m, &it)) {
        if (it.key != key) {
            kvp_add(&w, it.key, it.type, it.value, it.size);
            continue;
        }
        if (!done && how == NULL_KEY)
            kvp_add_null(&w, key);
        if (!done && how == OTHER_KEY) {
            if (it.type == KVP_TEXT)
                kvp_add_int32(&w, key, 1);
            else
                kvp_add_text(&w, key, "x");
        }
        done = 1;
    }
    return kvp_end(&w, &len) == KVP_OK ? len : 0;
}

#define FAILS   0       /* the message is refused */
#define EMPTY   1       /* read, and the field is not written again */
#define IS_NULL 2       /* read, and the field is written again as a null */

/* Takes the full message and, key by key, drops it, sends it as a null and
 * sends it with another type: per form, whether the message is still read
 * and what the field then holds, seen by writing again what was read. */
static void test_every_key_by_key(void)
{
    static const struct {
        int from, to;
        int want[3];            /* dropped, null, of another type */
    } forms[] = {
        {  1, 10, { FAILS, FAILS, FAILS } },        /* required */
        { 11, 20, { EMPTY, EMPTY, EMPTY } },        /* optional */
        { 21, 30, { FAILS, IS_NULL, FAILS } },      /* nullable */
        { 31, 40, { EMPTY, EMPTY, EMPTY } },        /* optional nullable: it reads null, and that null is not written */
        { 41, 50, { EMPTY, EMPTY, EMPTY } },        /* list */
        { 51, 51, { FAILS, FAILS, FAILS } },        /* tupleset */
        { 52, 52, { EMPTY, EMPTY, EMPTY } }         /* optional tupleset */
    };
    static uint8_t full[EVERY_MAX], bytes[EVERY_MAX], again[EVERY_MAX];
    size_t nfull = unhex(every_full_hex, full), n, len;
    kvp_buffer b;
    kvp_message m;
    kvp_item it;
    api_every e;
    const kvp_list_st *lists[10];
    const int *on_null[10];
    int form, key, how, want, rc, bad = 0;

    on_null[0] = &e.on_int32.null;
    on_null[1] = &e.on_int64.null;
    on_null[2] = &e.on_ts.null;
    on_null[3] = &e.on_text.null;
    on_null[4] = &e.on_bin.null;
    on_null[5] = &e.on_bool.null;
    on_null[6] = &e.on_float64.null;
    on_null[7] = &e.on_ds.null;
    on_null[8] = &e.on_msg.null;
    on_null[9] = &e.on_rec.null;
    lists[0] = &e.l_int32;
    lists[1] = &e.l_int64;
    lists[2] = &e.l_ts;
    lists[3] = &e.l_text;
    lists[4] = &e.l_bin;
    lists[5] = &e.l_bool;
    lists[6] = &e.l_float64;
    lists[7] = &e.l_ds;
    lists[8] = &e.l_msg;
    lists[9] = &e.l_rec;

    for (form = 0; form < (int)(sizeof forms / sizeof forms[0]); form++) {
        for (key = forms[form].from; key <= forms[form].to; key++) {
            for (how = DROP_KEY; how <= OTHER_KEY; how++) {
                want = forms[form].want[how];
                n = change(full, nfull, (uint16_t)key, how, bytes, sizeof bytes);
                kvp_buffer_wrap(&b, bytes, n);
                rc = api_every_decode(&b, &e);
                if (want == FAILS) {
                    if (rc != KVP_E_MISSING) {
                        bad++;
                        printf("key %d, case %d: read (%d), want it refused\n", key, how, rc);
                    }
                    continue;
                }
                if (rc != KVP_OK) {
                    bad++;
                    printf("key %d, case %d: refused (%d)\n", key, how, rc);
                    continue;
                }
                if (key >= 41 && key <= 50)             /* a list: nothing of it is left */
                    bad += lists[key - 41]->count != 0;
                if (key == 52)
                    bad += e.o_set.nfields != 0;
                /* what was read, written again */
                len = 0;
                if (api_every_encode(&e, &b, again, sizeof again, &len) != KVP_OK || kvp_parse(again, len, &m) != KVP_OK) {
                    bad++;
                    printf("key %d, case %d: not written again\n", key, how);
                    continue;
                }
                rc = kvp_find(&m, (uint16_t)key, &it);
                if (want == EMPTY && rc != KVP_E_NOT_FOUND) {
                    bad++;
                    printf("key %d, case %d: the field holds something\n", key, how);
                }
                if (want == IS_NULL && kvp_is_null(&m, (uint16_t)key) != 1) {
                    bad++;
                    printf("key %d, case %d: the field is not null\n", key, how);
                }
                /* the null of an optional nullable field is there to see */
                if (key >= 31 && key <= 40 && !*on_null[key - 31]) {
                    bad++;
                    printf("key %d, case %d: its null is not set\n", key, how);
                }
            }
        }
    }
    CHECK(bad == 0);
}

int main(void)
{
    test_write_canonical();
    test_read_canonical();
    test_query();
    test_big();
    test_no_fields();
    test_errors();
    test_every_write();
    test_every_read();
    test_every_key_by_key();
    if (failures) {
        printf("kvpack generated C: %d failure(s)\n", failures);
        return 1;
    }
    printf("kvpack generated C: all good (%d-bit)\n", (int)(sizeof(void *) * 8));
    return 0;
}

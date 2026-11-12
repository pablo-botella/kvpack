/*
 * test_kvpack.c — exercises the C writer and reader: the canonical
 * message shared with the Go and Harbour tests (same bytes), a round trip
 * with lists and nested messages, and every rejection. Exit code 0 when
 * all good; each failure prints its line.
 *   build.bat    (MSVC)   or   make    (gcc / MinGW)
 */
#include "kvpack.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) do { if (!(cond)) { failures++; printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #cond); } } while (0)

/* The canonical message: command 7, int32 -2, int64 1234567890123,
 * ts 1700000000, text "héllo" (UTF-8), bin 00 ff 10, under keys 1..5. */
static const uint8_t canonical[] = {
    0x4b, 0x56, 0x50, 0x01, 0x07, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x45, 0x00, 0x00, 0x00,
    0xa1, 0x23, 0x31, 0x48, 0x01, 0x00, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0xfe, 0xff, 0xff, 0xff,
    0x02, 0x00, 0x02, 0x00, 0x08, 0x00, 0x00, 0x00, 0xcb, 0x04, 0xfb, 0x71, 0x1f, 0x01, 0x00, 0x00,
    0x03, 0x00, 0x03, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0xf1, 0x53, 0x65, 0x00, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x04, 0x00, 0x06, 0x00, 0x00, 0x00, 0x68, 0xc3, 0xa9, 0x6c, 0x6c, 0x6f, 0x05, 0x00,
    0x05, 0x00, 0x03, 0x00, 0x00, 0x00, 0x00, 0xff, 0x10
};

/* The second canonical message, the later types: command 8, bool true,
 * bool false, float64 1.5, ds "20261111", null ds (8 spaces), under keys 1..5. */
static const uint8_t canonical2[] = {
    0x4b, 0x56, 0x50, 0x01, 0x08, 0x00, 0x00, 0x00, 0x05, 0x00, 0x00, 0x00, 0x42, 0x00, 0x00, 0x00,
    0x90, 0x00, 0xfc, 0x76, 0x01, 0x00, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x02, 0x00, 0x07,
    0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x08, 0x00, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00,
    0x00, 0x00, 0x00, 0x00, 0xf8, 0x3f, 0x04, 0x00, 0x09, 0x00, 0x08, 0x00, 0x00, 0x00, 0x32, 0x30,
    0x32, 0x36, 0x31, 0x31, 0x31, 0x31, 0x05, 0x00, 0x09, 0x00, 0x08, 0x00, 0x00, 0x00, 0x20, 0x20,
    0x20, 0x20, 0x20, 0x20, 0x20, 0x20
};

static void reseal(uint8_t *b, size_t len);

static void test_canonical2(void)
{
    uint8_t buf[128], b[sizeof canonical2];
    kvp_writer w;
    kvp_message m;
    size_t len = 0;
    int bv = -1;
    double f = 0;
    int32_t ds = -1;

    kvp_begin(&w, buf, sizeof buf, 8);
    kvp_add_bool(&w, 1, 7);                   /* nonzero is true */
    kvp_add_bool(&w, 2, 0);
    kvp_add_float64(&w, 3, 1.5);
    kvp_add_ds(&w, 4, 20261111);
    kvp_add_ds(&w, 5, 0);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(len == sizeof canonical2);
    CHECK(memcmp(buf, canonical2, sizeof canonical2) == 0);

    CHECK(kvp_parse(canonical2, sizeof canonical2, &m) == KVP_OK);
    CHECK(kvp_get_bool(&m, 1, &bv) == KVP_OK && bv == 1);
    CHECK(kvp_get_bool(&m, 2, &bv) == KVP_OK && bv == 0);
    CHECK(kvp_get_float64(&m, 3, &f) == KVP_OK && f == 1.5);
    CHECK(kvp_get_ds(&m, 4, &ds) == KVP_OK && ds == 20261111);
    CHECK(kvp_get_ds(&m, 5, &ds) == KVP_OK && ds == 0);
    CHECK(kvp_get_bool(&m, 3, &bv) == KVP_E_TYPE);

    /* a bool other than 0/1 is refused */
    memcpy(b, canonical2, sizeof b);
    b[28] = 2;
    reseal(b, sizeof b);
    CHECK(kvp_parse(b, sizeof b, &m) == KVP_E_FORMAT);

    /* like SToD, a ds that is no real day reads as the null date: the value
     * of key 4 lies at 62..69 */
    {
        static const char *const bad[] = { "20260230", "20261311", "00000000", "2026111x", "2026    ", NULL };
        const char *const *s;
        for (s = bad; *s; s++) {
            memcpy(b, canonical2, sizeof b);
            memcpy(b + 62, *s, 8);
            reseal(b, sizeof b);
            ds = -1;
            CHECK(kvp_parse(b, sizeof b, &m) == KVP_OK);
            CHECK(kvp_get_ds(&m, 4, &ds) == KVP_OK && ds == 0);
        }
    }

    /* and what is no real day is written as the null date */
    {
        static const int32_t bad[] = { 20260230, 20261311, 20250229, 20260000, -1, 100000000 };
        size_t i;
        for (i = 0; i < sizeof bad / sizeof bad[0]; i++) {
            kvp_begin(&w, buf, sizeof buf, 8);
            CHECK(kvp_add_ds(&w, 1, bad[i]) == KVP_OK);
            CHECK(memcmp(buf + KVP_HEADER_SIZE + KVP_ITEM_SIZE, "        ", 8) == 0);
        }
    }
    kvp_begin(&w, buf, sizeof buf, 8);
    CHECK(kvp_add_ds(&w, 1, 20240229) == KVP_OK && kvp_add_ds(&w, 2, 99991231) == KVP_OK
          && kvp_add_ds(&w, 3, 10101) == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK && kvp_parse(buf, len, &m) == KVP_OK);
    CHECK(kvp_get_ds(&m, 1, &ds) == KVP_OK && ds == 20240229);
    CHECK(kvp_get_ds(&m, 3, &ds) == KVP_OK && ds == 10101);
}

/* The third canonical message, the null type: command 9, int32 7, null,
 * an empty text, under keys 1..3. */
static const uint8_t canonical3[] = {
    0x4b, 0x56, 0x50, 0x01, 0x09, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00, 0x1c, 0x00, 0x00, 0x00,
    0xa2, 0xae, 0x13, 0xae, 0x01, 0x00, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0x07, 0x00, 0x00, 0x00,
    0x02, 0x00, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0x00, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00
};

static void test_canonical3(void)
{
    uint8_t buf[64], b[sizeof canonical3];
    kvp_writer w;
    kvp_message m;
    kvp_item it;
    kvp_utf8 t;
    size_t len = 0;
    int32_t i32 = 0;

    kvp_begin(&w, buf, sizeof buf, 9);
    kvp_add_int32(&w, 1, 7);
    kvp_add_null(&w, 2);
    kvp_add_text(&w, 3, "");
    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(len == sizeof canonical3);
    CHECK(memcmp(buf, canonical3, sizeof canonical3) == 0);

    CHECK(kvp_parse(canonical3, sizeof canonical3, &m) == KVP_OK);
    CHECK(kvp_is_null(&m, 2) == 1);
    CHECK(kvp_is_null(&m, 1) == 0 && kvp_is_null(&m, 3) == 0);
    CHECK(kvp_is_null(&m, 9) == 0);                             /* absent is not null */
    CHECK(kvp_find(&m, 2, &it) == KVP_OK && it.type == KVP_NULL && it.size == 0);
    CHECK(kvp_get_int32(&m, 2, &i32) == KVP_E_TYPE);            /* never reinterpreted */
    CHECK(kvp_get_utf8(&m, 2, &t) == KVP_E_TYPE);
    CHECK(kvp_get_utf8(&m, 3, &t) == KVP_OK && t.len == 0);
    CHECK(strcmp(kvp_type_str(KVP_NULL), "null") == 0);

    /* a null carries no value: the size of key 2 lies at 36; the byte it
     * claims is taken from the next item, so nitems drops to 2 */
    memcpy(b, canonical3, sizeof b);
    b[36] = 8;
    b[8] = 2;
    reseal(b, sizeof b);
    CHECK(kvp_parse(b, sizeof b, &m) == KVP_E_FORMAT);
    kvp_begin(&w, buf, sizeof buf, 9);
    CHECK(kvp_add(&w, 1, KVP_NULL, "x", 1) == KVP_E_TYPE);
}

/* The fourth canonical message, tuples: command 10. Under key 1 a
 * definition (id int32, name text with meta "len=40", born ds) and two
 * tuples: 1, "Ana", 1980-01-15 and 2, "Luis", null. Under key 2 a
 * definition (n int64) with no tuples. */
static const uint8_t canonical4[] = {
    0x4b, 0x56, 0x50, 0x01, 0x0a, 0x00, 0x00, 0x00, 0x04, 0x00, 0x00, 0x00, 0x9c, 0x00, 0x00, 0x00,
    0xf1, 0x58, 0xc3, 0x5d, 0x01, 0x00, 0x0b, 0x00, 0x32, 0x00, 0x00, 0x00, 0x03, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 0x69, 0x64, 0x00, 0x00, 0x00, 0x00, 0x04, 0x00, 0x04, 0x00,
    0x00, 0x00, 0x6e, 0x61, 0x6d, 0x65, 0x06, 0x00, 0x00, 0x00, 0x6c, 0x65, 0x6e, 0x3d, 0x34, 0x30,
    0x09, 0x00, 0x04, 0x00, 0x00, 0x00, 0x62, 0x6f, 0x72, 0x6e, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00,
    0x0c, 0x00, 0x21, 0x00, 0x00, 0x00, 0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x03, 0x00, 0x00, 0x00, 0x41, 0x6e, 0x61, 0x09, 0x00, 0x08, 0x00, 0x00, 0x00, 0x31,
    0x39, 0x38, 0x30, 0x30, 0x31, 0x31, 0x35, 0x01, 0x00, 0x0c, 0x00, 0x1a, 0x00, 0x00, 0x00, 0x01,
    0x00, 0x04, 0x00, 0x00, 0x00, 0x02, 0x00, 0x00, 0x00, 0x04, 0x00, 0x04, 0x00, 0x00, 0x00, 0x4c,
    0x75, 0x69, 0x73, 0x0a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x00, 0x0b, 0x00, 0x0f, 0x00, 0x00,
    0x00, 0x01, 0x00, 0x00, 0x00, 0x02, 0x00, 0x01, 0x00, 0x00, 0x00, 0x6e, 0x00, 0x00, 0x00, 0x00
};

static int utf8_is(const kvp_utf8 *t, const char *s)
{
    return t->len == strlen(s) && memcmp(t->p, s, t->len) == 0;
}

static void test_canonical4(void)
{
    uint8_t buf[256];
    kvp_writer w, def, row;
    kvp_message m;
    kvp_tupledef d;
    kvp_field fd;
    kvp_item it, f;
    kvp_utf8 t;
    size_t len = 0;
    int32_t i32 = 0;
    int n = 0;

    kvp_begin(&w, buf, sizeof buf, 10);
    CHECK(kvp_begin_tupledef(&w, 1, &def) == KVP_OK);
    CHECK(kvp_tupledef_add(&def, KVP_INT32, "id", NULL, 0) == KVP_OK);
    CHECK(kvp_tupledef_add(&def, KVP_TEXT, "name", "len=40", KVP_STRLEN) == KVP_OK);   /* a meta that is a string */
    CHECK(kvp_tupledef_add(&def, KVP_DS, "born", NULL, KVP_STRLEN) == KVP_OK);         /* and none */
    CHECK(kvp_end_tupledef(&w, &def) == KVP_OK);

    CHECK(kvp_begin_tuple(&w, 1, &row) == KVP_OK);
    kvp_tuple_int32(&row, 1);
    kvp_tuple_text(&row, "Ana");
    kvp_tuple_ds(&row, 19800115);
    CHECK(kvp_end_tuple(&w, &row) == KVP_OK);

    CHECK(kvp_begin_tuple(&w, 1, &row) == KVP_OK);
    kvp_tuple_int32(&row, 2);
    t = kvp_utf8_of("Luis");
    kvp_tuple_utf8(&row, &t);
    kvp_tuple_null(&row);
    CHECK(kvp_end_tuple(&w, &row) == KVP_OK);

    CHECK(kvp_begin_tupledef(&w, 2, &def) == KVP_OK);
    CHECK(kvp_tupledef_add(&def, KVP_INT64, "n", NULL, 0) == KVP_OK);
    CHECK(kvp_end_tupledef(&w, &def) == KVP_OK);

    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(len == sizeof canonical4);
    CHECK(memcmp(buf, canonical4, sizeof canonical4) == 0);

    CHECK(kvp_parse(canonical4, sizeof canonical4, &m) == KVP_OK);
    CHECK(kvp_get_tupledef(&m, 1, &d) == KVP_OK && d.nfields == 3);
    CHECK(kvp_tupledef_field(&d, 0, &fd) == KVP_OK && fd.type == KVP_INT32 && utf8_is(&fd.name, "id") && fd.meta.len == 0);
    CHECK(kvp_tupledef_field(&d, 1, &fd) == KVP_OK && fd.type == KVP_TEXT && utf8_is(&fd.name, "name"));
    CHECK(fd.meta.len == 6 && memcmp(fd.meta.p, "len=40", 6) == 0);
    CHECK(kvp_tupledef_field(&d, 2, &fd) == KVP_OK && fd.type == KVP_DS && utf8_is(&fd.name, "born"));
    CHECK(kvp_tupledef_field(&d, 3, &fd) == KVP_E_NOT_FOUND);

    for (it = d.item; kvp_tuple_next(&m, &it) == KVP_OK; ) {
        n++;
        CHECK(kvp_tuple_field(&d, &it, 0, &f) == KVP_OK && f.key == 1 && kvp_int32(&f, &i32) == KVP_OK && i32 == n);
        CHECK(kvp_tuple_field(&d, &it, 1, &f) == KVP_OK && kvp_item_utf8(&f, &t) == KVP_OK);
        CHECK(utf8_is(&t, n == 1 ? "Ana" : "Luis"));
        CHECK(kvp_tuple_field(&d, &it, 2, &f) == KVP_OK);
        if (n == 1) {
            CHECK(f.type == KVP_DS && kvp_ds(&f, &i32) == KVP_OK && i32 == 19800115);
        } else {
            CHECK(f.type == KVP_NULL && f.size == 0);
            CHECK(kvp_ds(&f, &i32) == KVP_E_TYPE);              /* a null is never a value */
        }
        CHECK(kvp_tuple_field(&d, &it, 3, &f) == KVP_E_NOT_FOUND);
        CHECK(kvp_tuple_field(&d, &it, 0, &f) == KVP_OK && kvp_item_utf8(&f, &t) == KVP_E_TYPE);
    }
    CHECK(n == 2);

    /* the definition with no tuples */
    CHECK(kvp_get_tupledef(&m, 2, &d) == KVP_OK && d.nfields == 1);
    it = d.item;
    CHECK(kvp_tuple_next(&m, &it) == KVP_E_NOT_FOUND);
    CHECK(kvp_get_tupledef(&m, 9, &d) == KVP_E_NOT_FOUND);
    CHECK(strcmp(kvp_type_str(KVP_TUPLEDEF), "tupledef") == 0 && strcmp(kvp_type_str(KVP_TUPLE), "tuple") == 0);
}

/* put_def and put_row write, raw, a definition (n int32, s text) and one
 * tuple of it (1, "a"): the order tests below place them at will. */
static const uint8_t raw_def[] = {
    0x02, 0x00, 0x00, 0x00,
    0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 'n', 0x00, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 's', 0x00, 0x00, 0x00, 0x00
};
static const uint8_t raw_row[] = {
    0x01, 0x00, 0x04, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00,
    0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 'a'
};

static int parses(kvp_writer *w)
{
    kvp_message m;
    size_t len = 0;
    if (kvp_end(w, &len) != KVP_OK)
        return -100;
    return kvp_parse(w->buf, len, &m);
}

static void test_tuple_order(void)
{
    uint8_t buf[256], d[sizeof raw_def + 1];
    kvp_writer w, def, row;
    int32_t one = 1;

    /* a reader holds tuples to their order */
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, raw_row, sizeof raw_row);
    kvp_add(&w, 1, KVP_TUPLE, raw_row, sizeof raw_row);
    kvp_add(&w, 2, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 2, KVP_TUPLE, raw_row, sizeof raw_row);
    kvp_add_int32(&w, 5, 1);
    CHECK(parses(&w) == KVP_OK);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* no definition */
    kvp_add(&w, 1, KVP_TUPLE, raw_row, sizeof raw_row);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* under another key */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 2, KVP_TUPLE, raw_row, sizeof raw_row);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* an item in between */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add_int32(&w, 5, 1);
    kvp_add(&w, 1, KVP_TUPLE, raw_row, sizeof raw_row);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* two definitions under a key */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 2, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* one field short */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, raw_row, 10);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* an int32 field of 3 bytes */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, "\x01\x00\x03\x00\x00\x00\x01\x00\x00\x04\x00\x00\x00\x00\x00", 15);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* a field size past the end */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, "\x01\x00\x04\x00\x00\x00\x01\x00\x00\x00\x04\x00\x09\x00\x00\x00" "a", 17);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* a field of another type than declared */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, "\x04\x00\x04\x00\x00\x00\x01\x00\x00\x00\x04\x00\x01\x00\x00\x00" "a", 17);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* a null that carries a value */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, "\x0a\x00\x01\x00\x00\x00\x00\x04\x00\x01\x00\x00\x00" "a", 14);
    CHECK(parses(&w) == KVP_E_FORMAT);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* a null in each field is fine */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def);
    kvp_add(&w, 1, KVP_TUPLE, "\x0a\x00\x00\x00\x00\x00\x0a\x00\x00\x00\x00\x00", 12);
    CHECK(parses(&w) == KVP_OK);

    kvp_begin(&w, buf, sizeof buf, 1);                          /* a definition cut short */
    kvp_add(&w, 1, KVP_TUPLEDEF, raw_def, sizeof raw_def - 1);
    CHECK(parses(&w) == KVP_E_FORMAT);

    memcpy(d, raw_def, sizeof raw_def);                         /* with bytes left over */
    d[sizeof raw_def] = 0;
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_add(&w, 1, KVP_TUPLEDEF, d, sizeof d);
    CHECK(parses(&w) == KVP_E_FORMAT);

    memcpy(d, raw_def, sizeof raw_def);                         /* a repeated name */
    d[21] = 'n';
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_add(&w, 1, KVP_TUPLEDEF, d, sizeof raw_def);
    CHECK(parses(&w) == KVP_E_FORMAT);

    memcpy(d, raw_def, sizeof raw_def);                         /* null as a field type */
    d[15] = KVP_NULL;
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_add(&w, 1, KVP_TUPLEDEF, d, sizeof raw_def);
    CHECK(parses(&w) == KVP_E_FORMAT);

    /* and so does the writer */
    kvp_begin(&w, buf, sizeof buf, 1);
    CHECK(kvp_begin_tuple(&w, 1, &row) == KVP_E_FORMAT);        /* no definition */

    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_begin_tupledef(&w, 1, &def);
    CHECK(kvp_tupledef_add(&def, KVP_INT32, "n", NULL, 0) == KVP_OK);
    CHECK(kvp_tupledef_add(&def, KVP_TEXT, "n", NULL, 0) == KVP_E_ARGUMENT);    /* repeated name */
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_begin_tupledef(&w, 1, &def);
    CHECK(kvp_tupledef_add(&def, KVP_INT32, "", NULL, 0) == KVP_E_ARGUMENT);
    kvp_begin(&w, buf, sizeof buf, 1);
    kvp_begin_tupledef(&w, 1, &def);
    CHECK(kvp_tupledef_add(&def, KVP_TUPLE, "t", NULL, 0) == KVP_E_TYPE);
    CHECK(kvp_end_tupledef(&w, &def) == KVP_E_TYPE);            /* the failure reaches the message */

#define OPEN_DEF() \
    kvp_begin(&w, buf, sizeof buf, 1); \
    kvp_begin_tupledef(&w, 1, &def); \
    kvp_tupledef_add(&def, KVP_INT32, "n", NULL, 0); \
    kvp_tupledef_add(&def, KVP_TEXT, "s", NULL, 0); \
    CHECK(kvp_end_tupledef(&w, &def) == KVP_OK)

    OPEN_DEF();
    CHECK(kvp_begin_tupledef(&w, 1, &def) == KVP_E_FORMAT);     /* a second definition */

    OPEN_DEF();
    CHECK(kvp_begin_tuple(&w, 2, &row) == KVP_E_FORMAT);        /* another key */

    OPEN_DEF();
    kvp_add_int32(&w, 5, one);
    CHECK(kvp_begin_tuple(&w, 1, &row) == KVP_E_FORMAT);        /* an item in between */

    OPEN_DEF();
    kvp_begin_tuple(&w, 1, &row);
    CHECK(kvp_tuple_text(&row, "x") == KVP_E_TYPE);             /* a text for the int32 */
    CHECK(kvp_end_tuple(&w, &row) == KVP_E_TYPE);

    OPEN_DEF();
    kvp_begin_tuple(&w, 1, &row);
    kvp_tuple_int32(&row, 1);
    CHECK(kvp_end_tuple(&w, &row) == KVP_E_FORMAT);             /* one value short */

    OPEN_DEF();
    kvp_begin_tuple(&w, 1, &row);
    kvp_tuple_int32(&row, 1);
    kvp_tuple_null(&row);
    CHECK(kvp_tuple_null(&row) == KVP_E_FORMAT);                /* one value too many */

    OPEN_DEF();                                                 /* two tuples, then the message */
    kvp_begin_tuple(&w, 1, &row);
    kvp_tuple_null(&row);
    kvp_tuple_text(&row, "");
    CHECK(kvp_end_tuple(&w, &row) == KVP_OK);
    kvp_begin_tuple(&w, 1, &row);
    kvp_tuple_int32(&row, 7);
    kvp_tuple_null(&row);
    CHECK(kvp_end_tuple(&w, &row) == KVP_OK);
    CHECK(parses(&w) == KVP_OK);
#undef OPEN_DEF

    /* a small buffer fails as any other add */
    kvp_begin(&w, buf, 40, 1);
    kvp_begin_tupledef(&w, 1, &def);
    CHECK(kvp_tupledef_add(&def, KVP_TEXT, "a_long_field_name", NULL, 0) == KVP_E_FULL);
    CHECK(kvp_end_tupledef(&w, &def) == KVP_E_FULL);
}

static void test_write_canonical(void)
{
    uint8_t buf[256];
    kvp_writer w;
    size_t len = 0;
    static const uint8_t three[] = { 0x00, 0xff, 0x10 };
    CHECK(kvp_begin(&w, buf, sizeof buf, 7) == KVP_OK);
    CHECK(kvp_add_int32(&w, 1, -2) == KVP_OK);
    CHECK(kvp_add_int64(&w, 2, (int64_t)1234567890 * 1000 + 123) == KVP_OK);
    CHECK(kvp_add_ts(&w, 3, (int64_t)1700000000) == KVP_OK);
    CHECK(kvp_add_text(&w, 4, "h\xc3\xa9llo") == KVP_OK);
    CHECK(kvp_add_bin(&w, 5, three, 3) == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(len == sizeof canonical);
    CHECK(memcmp(buf, canonical, sizeof canonical) == 0);
    CHECK(kvp_room(5, 4 + 8 + 8 + 6 + 3) == sizeof canonical);
}

static void test_read_canonical(void)
{
    kvp_message m;
    kvp_item it;
    int32_t i32 = 0;
    int64_t i64 = 0, ts = 0;
    char text[16];
    const void *bytes = NULL;
    uint32_t size = 0;
    int n = 0, rc;

    CHECK(kvp_parse(canonical, sizeof canonical, &m) == KVP_OK);
    CHECK(m.command == 7 && m.nitems == 5 && m.size == 69 && m.crc32 == 0x483123a1u);
    CHECK(m.len == sizeof canonical);

    CHECK(kvp_get_int32(&m, 1, &i32) == KVP_OK && i32 == -2);
    CHECK(kvp_get_int64(&m, 2, &i64) == KVP_OK && i64 == (int64_t)1234567890 * 1000 + 123);
    CHECK(kvp_get_ts(&m, 3, &ts) == KVP_OK && ts == (int64_t)1700000000);
    CHECK(kvp_get_text(&m, 4, text, sizeof text) == KVP_OK && strcmp(text, "h\xc3\xa9llo") == 0);
    CHECK(kvp_get_bin(&m, 5, &bytes, &size) == KVP_OK && size == 3 && ((const uint8_t *)bytes)[1] == 0xff);

    /* wrong type, absent key, truncated text */
    CHECK(kvp_get_text(&m, 1, text, sizeof text) == KVP_E_TYPE);
    CHECK(kvp_get_int32(&m, 9, &i32) == KVP_E_NOT_FOUND);
    CHECK(kvp_get_text(&m, 4, text, 3) == KVP_E_FULL && strcmp(text, "h\xc3") == 0);
    CHECK(kvp_error_text(&m, text, sizeof text) == KVP_E_NOT_FOUND);

    /* the walk sees the five items in order */
    for (rc = kvp_first(&m, &it); rc == KVP_OK; rc = kvp_next(&m, &it)) {
        n++;
        CHECK(it.key == n && it.type == n);
    }
    CHECK(rc == KVP_E_NOT_FOUND && n == 5);

    /* header only */
    CHECK(kvp_header(canonical, KVP_HEADER_SIZE, &m) == KVP_OK && m.size == 69);
    CHECK(kvp_header(canonical, 10, &m) == KVP_E_TRUNCATED);
}

static void test_lists_and_nested(void)
{
    uint8_t row1[64], row2[64], buf[256];
    kvp_writer w;
    size_t l1, l2, len;
    kvp_message m, sub;
    kvp_item it;
    int32_t id = 0;
    char text[16];
    int n = 0, rc;

    CHECK(kvp_begin(&w, row1, sizeof row1, 100) == KVP_OK);
    CHECK(kvp_add_int32(&w, 1, 1) == KVP_OK);
    CHECK(kvp_add_text(&w, 2, "a") == KVP_OK);
    CHECK(kvp_end(&w, &l1) == KVP_OK);
    CHECK(kvp_begin(&w, row2, sizeof row2, 100) == KVP_OK);
    CHECK(kvp_add_int32(&w, 1, 2) == KVP_OK);
    CHECK(kvp_add_text(&w, 2, "b") == KVP_OK);
    CHECK(kvp_end(&w, &l2) == KVP_OK);

    CHECK(kvp_begin(&w, buf, sizeof buf, 5) == KVP_OK);
    CHECK(kvp_add_msg(&w, 1, row1, l1) == KVP_OK);
    CHECK(kvp_add_msg(&w, 1, row2, l2) == KVP_OK);
    CHECK(kvp_add_text(&w, 2, "tail") == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK);

    CHECK(kvp_parse(buf, len, &m) == KVP_OK);
    for (rc = kvp_find(&m, 1, &it); rc == KVP_OK; rc = kvp_find_next(&m, &it)) {
        n++;
        CHECK(kvp_msg(&it, &sub) == KVP_OK && sub.command == 100);
        CHECK(kvp_get_int32(&sub, 1, &id) == KVP_OK && id == n);
    }
    CHECK(n == 2);
    CHECK(kvp_get_msg(&m, 1, &sub) == KVP_OK && kvp_get_text(&sub, 2, text, sizeof text) == KVP_OK && strcmp(text, "a") == 0);
    CHECK(kvp_get_msg(&m, 2, &sub) == KVP_E_TYPE);

    /* a corrupt nested message makes the parent unparsable */
    CHECK(kvp_begin(&w, buf, sizeof buf, 5) == KVP_OK);
    CHECK(kvp_add(&w, 1, KVP_MSG, "KVP\x01junk", 8) == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(kvp_parse(buf, len, &m) == KVP_E_FORMAT);

    /* an error reply */
    CHECK(kvp_begin(&w, buf, sizeof buf, 9) == KVP_OK);
    CHECK(kvp_add_text(&w, KVP_KEY_ERROR, "no such user") == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(kvp_parse(buf, len, &m) == KVP_OK);
    CHECK(kvp_error_text(&m, text, sizeof text) == KVP_OK && strcmp(text, "no such user") == 0);

    /* an empty message */
    CHECK(kvp_begin(&w, buf, sizeof buf, 3) == KVP_OK);
    CHECK(kvp_end(&w, &len) == KVP_OK && len == KVP_HEADER_SIZE);
    CHECK(kvp_parse(buf, len, &m) == KVP_OK && m.nitems == 0);
    CHECK(kvp_first(&m, &it) == KVP_E_NOT_FOUND);
}

static void test_writer_limits(void)
{
    uint8_t buf[40];
    kvp_writer w;
    size_t len;
    CHECK(kvp_begin(&w, buf, 10, 1) == KVP_E_FULL);
    CHECK(kvp_begin(&w, NULL, 100, 1) == KVP_E_ARGUMENT);
    CHECK(kvp_begin(&w, buf, sizeof buf, 1) == KVP_OK);
    CHECK(kvp_add_text(&w, 1, "0123456789ab") == KVP_OK);     /* 20 + 8 + 12 = 40 */
    CHECK(kvp_add_int32(&w, 2, 1) == KVP_E_FULL);
    CHECK(kvp_add_int32(&w, 3, 1) == KVP_E_FULL);              /* stays failed */
    CHECK(kvp_end(&w, &len) == KVP_E_FULL);
    CHECK(kvp_begin(&w, buf, sizeof buf, 1) == KVP_OK);
    CHECK(kvp_add(&w, 1, KVP_INT32, "abc", 3) == KVP_E_TYPE);
    CHECK(kvp_begin(&w, buf, sizeof buf, 1) == KVP_OK);
    CHECK(kvp_add(&w, 1, 99, "abc", 3) == KVP_E_TYPE);
    CHECK(kvp_begin(&w, buf, sizeof buf, 1) == KVP_OK);
    CHECK(kvp_add_text(&w, 1, NULL) == KVP_E_ARGUMENT);
}

/* reseal recomputes the crc after a deliberate corruption, so the
 * structural checks are what rejects it. */
static void reseal(uint8_t *b, size_t len)
{
    uint32_t crc = kvp_crc32(0, b + KVP_HEADER_SIZE, len - KVP_HEADER_SIZE);
    b[16] = (uint8_t)crc; b[17] = (uint8_t)(crc >> 8); b[18] = (uint8_t)(crc >> 16); b[19] = (uint8_t)(crc >> 24);
}

static void test_reader_rejects(void)
{
    uint8_t b[sizeof canonical + 1];
    kvp_message m;

    memcpy(b, canonical, sizeof canonical);
    b[0] ^= 0xff;
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_MAGIC);

    memcpy(b, canonical, sizeof canonical);
    b[3] = 2;
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_VERSION);

    CHECK(kvp_parse(canonical, 10, &m) == KVP_E_TRUNCATED);
    CHECK(kvp_parse(canonical, sizeof canonical - 1, &m) == KVP_E_TRUNCATED);
    memcpy(b, canonical, sizeof canonical);
    b[sizeof canonical] = 0;
    CHECK(kvp_parse(b, sizeof canonical + 1, &m) == KVP_E_FORMAT);

    memcpy(b, canonical, sizeof canonical);
    b[sizeof canonical - 1] ^= 0xff;
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_CRC);

    memcpy(b, canonical, sizeof canonical);
    b[8] = 6;                                   /* nitems too big */
    reseal(b, sizeof canonical);
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_FORMAT);

    memcpy(b, canonical, sizeof canonical);
    b[8] = 4;                                   /* nitems too small */
    reseal(b, sizeof canonical);
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_FORMAT);

    memcpy(b, canonical, sizeof canonical);
    b[24] = 3;                                  /* int32 with size 3 */
    reseal(b, sizeof canonical);
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_FORMAT);

    memcpy(b, canonical, sizeof canonical);
    b[22] = 99;                                 /* unknown type */
    reseal(b, sizeof canonical);
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_FORMAT);

    memcpy(b, canonical, sizeof canonical);
    b[sizeof canonical - 7] = 200;              /* last item's size past the end */
    reseal(b, sizeof canonical);
    CHECK(kvp_parse(b, sizeof canonical, &m) == KVP_E_FORMAT);

    CHECK(kvp_parse(NULL, 0, &m) == KVP_E_ARGUMENT);
    CHECK(strcmp(kvp_error_str(KVP_E_CRC), "crc32 mismatch") == 0);
    CHECK(strcmp(kvp_type_str(KVP_TS), "ts") == 0);
    CHECK(strcmp(kvp_type_str(KVP_MSG), "msg") == 0);
}

int main(void)
{
    test_write_canonical();
    test_read_canonical();
    test_lists_and_nested();
    test_writer_limits();
    test_reader_rejects();
    test_canonical2();
    test_canonical3();
    test_canonical4();
    test_tuple_order();
    if (failures) {
        printf("%d failure(s)\n", failures);
        return 1;
    }
    printf("kvpack C: all good (%u-bit)\n", (unsigned)(sizeof(void *) * 8));
    return 0;
}

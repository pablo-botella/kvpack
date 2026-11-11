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
    if (failures) {
        printf("%d failure(s)\n", failures);
        return 1;
    }
    printf("kvpack C: all good (%u-bit)\n", (unsigned)(sizeof(void *) * 8));
    return 0;
}

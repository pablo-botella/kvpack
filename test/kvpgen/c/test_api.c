/*
 * test_api.c - exercises the C code kvpack generates from test/kvpgen/api.kvp:
 * the canonical reply shared with the Go and Harbour tests, written and read
 * back, plus missing fields and error replies.
 */
#include "api_kvp.h"

#include <stdio.h>
#include <string.h>

static int failures;

#define CHECK(cond) \
    do { if (!(cond)) { failures++; printf("%s:%d: FAILED: %s\n", __FILE__, __LINE__, #cond); } } while (0)

static int text_is(const kvp_utf8 *t, const char *s)
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
    uint8_t want[512], buf[512];
    size_t nwant = unhex(canonical_hex, want), len = 0;
    kvp_writer w, sub;
    api_login_reply r;
    api_event ev;

    memset(&r, 0, sizeof r);
    r.name = kvp_utf8_of("ann");
    r.expires = 1700000000;
    r.owner.name = kvp_utf8_of("di");
    r.has_owner = 1;

    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    api_login_reply_write(&w, &r);

    memset(&ev, 0, sizeof ev);
    ev.id = 7;
    ev.title = kvp_utf8_of("a");
    ev.who.name = kvp_utf8_of("bo");
    api_login_reply_begin_events(&w, &sub, &ev);
    api_event_add_codes(&sub, 1);
    api_event_add_codes(&sub, 2);
    kvp_end_msg(&w, &sub);

    memset(&ev, 0, sizeof ev);
    ev.id = 8;
    ev.title = kvp_utf8_of("b");
    ev.big = 5;
    ev.who.name = kvp_utf8_of("cy");
    ev.active = 1;
    ev.score = 2.5;
    ev.day = 20261111;
    CHECK(api_login_reply_add_events(&w, &ev) == KVP_OK);

    CHECK(kvp_end(&w, &len) == KVP_OK);
    CHECK(len == nwant);
    CHECK(memcmp(buf, want, nwant) == 0);
}

static void test_read_canonical(void)
{
    uint8_t b[512];
    size_t n = unhex(canonical_hex, b);
    api_login_reply r;
    api_event ev;
    kvp_item it;
    int32_t code;
    int count = 0;

    CHECK(api_login_reply_decode(b, n, &r) == KVP_OK);
    CHECK(text_is(&r.name, "ann"));
    CHECK(r.expires == 1700000000);
    CHECK(r.level == 0);
    CHECK(r.has_owner == 1 && text_is(&r.owner.name, "di"));

    memset(&it, 0, sizeof it);
    while (api_login_reply_events(&r, &it, &ev) == KVP_OK) {
        count++;
        if (count == 1) {
            kvp_item ci;
            int ncodes = 0;
            CHECK(ev.id == 7 && text_is(&ev.title, "a") && ev.big == 0);
            CHECK(text_is(&ev.who.name, "bo"));
            CHECK(ev.blob.p == NULL && ev.raw.buf == NULL);
            CHECK(ev.active == 0 && ev.score == 0 && ev.day == 0);
            memset(&ci, 0, sizeof ci);
            while (api_event_codes(&ev, &ci, &code) == KVP_OK)
                CHECK(code == ++ncodes);
            CHECK(ncodes == 2);
        } else {
            CHECK(ev.id == 8 && text_is(&ev.title, "b") && ev.big == 5);
            CHECK(text_is(&ev.who.name, "cy"));
            CHECK(ev.active == 1 && ev.score == 2.5 && ev.day == 20261111);
        }
    }
    CHECK(count == 2);
}

static void test_errors(void)
{
    uint8_t buf[256];
    size_t len;
    kvp_writer w;
    api_login l;
    api_login_reply r;
    char text[32];

    /* password missing */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, API_LOGIN_USER, "bob");
    kvp_end(&w, &len);
    CHECK(api_login_decode(buf, len, &l) == KVP_E_MISSING);

    /* password of another type */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, API_LOGIN_USER, "bob");
    kvp_add_int32(&w, API_LOGIN_PASSWORD, 5);
    kvp_end(&w, &len);
    CHECK(api_login_decode(buf, len, &l) == KVP_E_MISSING);

    /* optional left out, unknown key ignored, encode in one call */
    memset(&l, 0, sizeof l);
    l.user = kvp_utf8_of("bob");
    l.password = kvp_utf8_of("x");
    CHECK(api_login_encode(&l, buf, sizeof buf, &len) == KVP_OK);
    CHECK(len == KVP_HEADER_SIZE + 2 * KVP_ITEM_SIZE + 4);
    CHECK(api_login_decode(buf, len, &l) == KVP_OK && l.when == 0 && text_is(&l.user, "bob"));

    /* error reply */
    kvp_begin(&w, buf, sizeof buf, API_CMD_LOGIN);
    kvp_add_text(&w, KVP_KEY_ERROR, "wrong password");
    kvp_end(&w, &len);
    CHECK(api_login_reply_decode(buf, len, &r) == KVP_E_REPLY);
    CHECK(kvp_error_text(&r.kvp, text, sizeof text) == KVP_OK && strcmp(text, "wrong password") == 0);

    /* buffer too small */
    memset(&l, 0, sizeof l);
    l.user = kvp_utf8_of("bob");
    l.password = kvp_utf8_of("x");
    CHECK(api_login_encode(&l, buf, 30, &len) == KVP_E_FULL);
}

int main(void)
{
    test_write_canonical();
    test_read_canonical();
    test_errors();
    if (failures) {
        printf("kvpack generated C: %d failure(s)\n", failures);
        return 1;
    }
    printf("kvpack generated C: all good (%d-bit)\n", (int)(sizeof(void *) * 8));
    return 0;
}

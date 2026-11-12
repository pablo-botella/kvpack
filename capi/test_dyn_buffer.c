/*
 * test_dyn_buffer.c — exercises dyn_buffer.c: growth by a doubling
 * granularity over the caller's malloc and free, room locked and unlocked,
 * what is in use read back, wrapped bytes, and no memory. The arena built on
 * it (kvp_arena.c) is exercised by the test of the generated code, in
 * test/kvpgen/c.
 * Exit code 0 when everything passes; each failure prints its line.
 */
#include "dyn_buffer.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;

#define CHECK(cond) \
    do { if (!(cond)) { failures++; printf("%s:%d: FAILED: %s\n", __FILE__, __LINE__, #cond); } } while (0)

static int allocs, frees;

static void *my_alloc(size_t n)
{
    allocs++;
    return malloc(n);
}

static void my_free(void *p)
{
    frees++;
    free(p);
}

static void *no_alloc(size_t n)
{
    (void)n;
    return NULL;
}

static void test_growth(void)
{
    kvp_buffer b;
    size_t len = 0;
    int i, bad = 0;

    /* the defaults: steps of 4096 that double up to 1 MB */
    kvp_buffer_init(&b, my_alloc, my_free, 0, 0);
    CHECK(b.data == NULL && b.size == 0 && b.gr == 4096 && b.grmax == 1048576 && allocs == 0);
    for (i = 0; i < 600000; i++)
        bad += kvp_buffer_append(&b, "abcdefghij", 10) != KVP_OK;
    CHECK(bad == 0 && b.size == 6000000 && b.capacity >= b.size && b.gr == 1048576);
    CHECK(memcmp(b.data, "abcdefghij", 10) == 0 && memcmp(b.data + 5999990, "abcdefghij", 10) == 0);
    CHECK(allocs < 20);                         /* a handful of blocks, not one per append */

    /* what is in use, and never past it */
    CHECK(kvp_buffer_getptr_at(&b, 0, &len) == b.data && len == 6000000);
    CHECK(kvp_buffer_getptr_at(&b, 5999990, &len) == b.data + 5999990 && len == 10);
    CHECK(kvp_buffer_getptr_at(&b, 6000000, &len) == b.data + 6000000 && len == 0);
    len = 99;
    CHECK(kvp_buffer_getptr_at(&b, 6000001, &len) == NULL && len == 0);
    CHECK(kvp_buffer_getptr_at(&b, 5, NULL) == b.data + 5);

    kvp_buffer_free(&b);
    CHECK(b.data == NULL && b.size == 0 && allocs == frees);
}

static void test_granularity(void)
{
    kvp_buffer b;

    /* less than 4096 is raised to it; the limit is taken as given */
    kvp_buffer_init(&b, my_alloc, my_free, 16, 45UL * 1024 * 1024);
    CHECK(b.gr == 4096 && b.grmax == 45UL * 1024 * 1024);
    CHECK(kvp_buffer_lock_bytes(&b, 1) != NULL && b.capacity == 4096 && b.gr == 8192 && b.size == 0);
    kvp_buffer_free(&b);

    /* changed along the way: it counts from the next growth on */
    kvp_buffer_init(&b, my_alloc, my_free, 0, 0);
    CHECK(kvp_buffer_lock_bytes(&b, 4096) != NULL && b.capacity == 4096);
    CHECK(kvp_buffer_unlock_bytes(&b, 4097) == KVP_E_ARGUMENT);        /* more than the room */
    CHECK(kvp_buffer_unlock_bytes(&b, 4096) == KVP_OK && b.size == 4096);
    kvp_buffer_set_granularity(&b, 50000, 100);                         /* a limit below gr is raised to gr */
    CHECK(b.gr == 50000 && b.grmax == 50000);
    CHECK(kvp_buffer_lock_bytes(&b, 1) != NULL && b.capacity == 54096 && b.gr == 50000);
    kvp_buffer_set_granularity(&b, 0, 0);
    CHECK(b.gr == 4096 && b.grmax == 1048576);
    kvp_buffer_free(&b);
    CHECK(allocs == frees);
}

/* A message of unknown size written straight into the buffer: lock room,
 * write, and when it does not fit lock more and write again. */
static void test_lock(void)
{
    kvp_buffer b;
    kvp_writer w;
    kvp_message m;
    size_t room = 1024, len = 0, max = 0;
    uint8_t *p;
    int i, tries = 0;

    kvp_buffer_init(&b, malloc, free, 0, 0);    /* the C library, as it is */
    CHECK(kvp_buffer_append(&b, "HEAD", 4) == KVP_OK);
    for (;;) {
        p = kvp_buffer_lock_bytes(&b, room);
        CHECK(p != NULL);
        if (!p)
            break;
        tries++;
        kvp_begin(&w, p, room, 5);
        for (i = 0; i < 1000; i++)
            kvp_add_text(&w, 1, "some text that takes room");
        if (kvp_end(&w, &len) == KVP_OK)
            break;
        room *= 2;
    }
    CHECK(tries > 1);
    CHECK(kvp_buffer_unlock_bytes(&b, len) == KVP_OK && b.size == 4 + len);
    p = kvp_buffer_getptr_at(&b, 4, &max);
    CHECK(p && max == len && kvp_parse(p, max, &m) == KVP_OK && m.nitems == 1000);
    CHECK(memcmp(kvp_buffer_getptr_at(&b, 0, NULL), "HEAD", 4) == 0);
    kvp_buffer_free(&b);
}

static void test_limits(void)
{
    kvp_buffer b;
    size_t len = 0;
    const char *bytes = "received";

    /* no memory: the buffer stays as it was */
    kvp_buffer_init(&b, no_alloc, my_free, 0, 0);
    CHECK(kvp_buffer_append(&b, "x", 1) == KVP_E_FULL && b.data == NULL && b.size == 0);
    CHECK(kvp_buffer_lock_bytes(&b, 1) == NULL);

    /* no functions */
    kvp_buffer_init(&b, NULL, NULL, 0, 0);
    CHECK(kvp_buffer_lock_bytes(&b, 1) == NULL && kvp_buffer_append(&b, "x", 1) == KVP_E_ARGUMENT);
    CHECK(kvp_buffer_append(&b, NULL, 1) == KVP_E_ARGUMENT);

    /* bytes the caller already has: all in use, read as any other, never grown, never freed */
    kvp_buffer_wrap(&b, bytes, 8);
    CHECK(b.size == 8 && kvp_buffer_getptr_at(&b, 2, &len) == (const uint8_t *)bytes + 2 && len == 6);
    CHECK(kvp_buffer_append(&b, "x", 1) != KVP_OK && b.size == 8);
    CHECK(kvp_buffer_lock_bytes(&b, 1) == NULL);
    kvp_buffer_free(&b);
    CHECK(b.data == NULL && b.size == 0);
}

int main(void)
{
    test_growth();
    test_granularity();
    test_lock();
    test_limits();
    if (failures) {
        printf("kvpack dyn_buffer: %d failure(s)\n", failures);
        return 1;
    }
    printf("kvpack dyn_buffer: all good (%d-bit)\n", (int)(sizeof(void *) * 8));
    return 0;
}

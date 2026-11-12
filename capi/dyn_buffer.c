/*
 * dyn_buffer.c — a buffer that grows through the application's malloc and
 * free. See dyn_buffer.h.
 */
#include "dyn_buffer.h"

#include <string.h>

#define KVP_BUFFER_GR    4096           /* the smallest granularity, and the one a 0 takes */
#define KVP_BUFFER_GRMAX 1048576UL      /* the limit a 0 takes: 1 MB */

void kvp_buffer_set_granularity(kvp_buffer *b, size_t gr, size_t grmax)
{
    if (!b)
        return;
    if (gr < KVP_BUFFER_GR)
        gr = KVP_BUFFER_GR;
    if (!grmax)
        grmax = KVP_BUFFER_GRMAX;
    if (grmax < gr)
        grmax = gr;
    b->gr = gr;
    b->grmax = grmax;
}

void kvp_buffer_init(kvp_buffer *b, void *(*pmalloc)(size_t), void (*pfree)(void *), size_t gr, size_t grmax)
{
    if (!b)
        return;
    b->pmalloc = pmalloc;
    b->pfree = pfree;
    b->data = NULL;
    b->size = 0;
    b->capacity = 0;
    kvp_buffer_set_granularity(b, gr, grmax);
}

void kvp_buffer_wrap(kvp_buffer *b, const void *bytes, size_t n)
{
    if (!b)
        return;
    kvp_buffer_init(b, NULL, NULL, 0, 0);       /* no functions: it cannot grow, and frees nothing */
    b->data = (uint8_t *)bytes;
    b->size = bytes ? n : 0;
    b->capacity = b->size;
}

/* grow makes room for n more bytes after the size in use: a new block of
 * as many steps of gr as it takes, the bytes copied, the old one freed. */
static int grow(kvp_buffer *b, size_t n)
{
    size_t need, extra, steps, cap;
    uint8_t *p;
    if (!b || !b->pmalloc || !b->pfree || !b->gr)
        return KVP_E_ARGUMENT;
    if (b->capacity - b->size >= n)
        return KVP_OK;
    need = b->size + n;
    if (need < b->size)                         /* size_t overflow */
        return KVP_E_FULL;
    extra = need - b->capacity;                 /* as many steps of gr as it takes */
    steps = extra / b->gr + (extra % b->gr != 0);
    cap = b->capacity + steps * b->gr;
    if (steps > ((size_t)-1 - b->capacity) / b->gr)
        cap = need;                             /* the rounding would overflow */
    p = (uint8_t *)b->pmalloc(cap);
    if (!p)
        return KVP_E_FULL;
    if (b->size)
        memcpy(p, b->data, b->size);
    if (b->data)
        b->pfree(b->data);
    b->data = p;
    b->capacity = cap;
    if (b->gr < b->grmax)                       /* double, up to grmax */
        b->gr = b->gr > b->grmax / 2 ? b->grmax : b->gr * 2;
    return KVP_OK;
}

uint8_t *kvp_buffer_lock_bytes(kvp_buffer *b, size_t n)
{
    if (grow(b, n) != KVP_OK)
        return NULL;
    return b->data ? b->data + b->size : NULL;
}

int kvp_buffer_unlock_bytes(kvp_buffer *b, size_t used)
{
    if (!b || used > b->capacity - b->size)
        return KVP_E_ARGUMENT;
    b->size += used;
    return KVP_OK;
}

int kvp_buffer_append(kvp_buffer *b, const void *bytes, size_t n)
{
    int rc;
    if (!bytes && n)
        return KVP_E_ARGUMENT;
    rc = grow(b, n);
    if (rc != KVP_OK)
        return rc;
    if (n)
        memcpy(b->data + b->size, bytes, n);
    b->size += n;
    return KVP_OK;
}

uint8_t *kvp_buffer_getptr_at(const kvp_buffer *b, size_t offset, size_t *max)
{
    if (max)
        *max = 0;
    if (!b || !b->data || offset > b->size)
        return NULL;
    if (max)
        *max = b->size - offset;
    return b->data + offset;
}

void kvp_buffer_free(kvp_buffer *b)
{
    if (!b)
        return;
    if (b->data && b->pfree)
        b->pfree(b->data);
    b->data = NULL;
    b->size = 0;
    b->capacity = 0;
}

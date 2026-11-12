/*
 * dyn_buffer.h — a buffer that grows, for callers that cannot size one in
 * advance. Optional: kvpack.c never allocates and does not need this file.
 * Nothing here calls malloc; the application hands in the two functions,
 * the C library's or its own:
 *
 *   kvp_buffer b;
 *   kvp_buffer_init(&b, malloc, free, 0, 0);
 *   p = kvp_buffer_lock_bytes(&b, 512);            room for up to 512 bytes, or NULL
 *   ...write n of them at p...
 *   kvp_buffer_unlock_bytes(&b, n);                n more bytes in use
 *   kvp_buffer_append(&b, bytes, n);               or copy some at the end
 *   p = kvp_buffer_getptr_at(&b, 0, &max);         what is in use: b.size bytes
 *   kvp_buffer_free(&b);
 *
 * ANSI C (C89), as kvpack.h.
 */
#ifndef DYN_BUFFER_H
#define DYN_BUFFER_H

#include "kvpack.h"

#ifdef __cplusplus
extern "C" {
#endif

/* The buffer: size bytes in use. capacity, the bytes allocated, is its own
 * business: a caller asks for room and reads what is in use, and never
 * needs it. pmalloc and pfree have the signatures of malloc and free:
 * pmalloc returns the bytes asked for or NULL when there is no memory,
 * pfree frees a block pmalloc returned. data moves when the buffer grows,
 * so no pointer into it survives a lock or an append.
 *
 * gr is the granularity: the buffer grows in steps of gr bytes (as many as
 * a request needs), and every time it grows gr doubles, until it reaches
 * grmax. Both are the application's to choose, at the start or at any
 * later time with kvp_buffer_set_granularity: they only matter the next
 * time the buffer grows. */
typedef struct {
    void  *(*pmalloc)(size_t size);
    void   (*pfree)(void *p);
    uint8_t *data;
    size_t   size;
    size_t   capacity;
    size_t   gr;
    size_t   grmax;
} kvp_buffer;

/* kvp_buffer_init starts an empty buffer over the two functions, with its
 * first granularity and the most it may reach by doubling, as
 * kvp_buffer_set_granularity takes them. Nothing is allocated yet. */
void kvp_buffer_init(kvp_buffer *b, void *(*pmalloc)(size_t), void (*pfree)(void *), size_t gr, size_t grmax);
/* kvp_buffer_wrap points a buffer at n bytes the caller already has (a
 * message just received, say), all of them in use, so that what reads from
 * a kvp_buffer can read them. Nothing is copied and nothing is owned: such
 * a buffer never grows (asking it for room fails) and kvp_buffer_free only
 * forgets the bytes, which must outlive it. */
void kvp_buffer_wrap(kvp_buffer *b, const void *bytes, size_t n);
/* kvp_buffer_set_granularity sets the granularity and the most it may
 * reach by doubling, from the next growth on. gr is never less than 4096
 * (a smaller one, or 0, becomes 4096); a grmax of 0 is 1 MB; and grmax is
 * never less than gr (a smaller one becomes gr, so the step does not
 * double). Set them through here rather than in the struct, which checks
 * nothing. */
void kvp_buffer_set_granularity(kvp_buffer *b, size_t gr, size_t grmax);
/* kvp_buffer_lock_bytes gives a pointer to room for up to n bytes right
 * after the size in use, growing the buffer (a new block, the bytes
 * copied, the old one freed) when they do not fit. NULL when there is no
 * memory: the buffer stays as it was. The size does not move until
 * kvp_buffer_unlock_bytes says how many of them were written; locking
 * again before that simply asks for the room anew (for more, say), and
 * what was written there is kept. */
uint8_t *kvp_buffer_lock_bytes(kvp_buffer *b, size_t n);
/* kvp_buffer_unlock_bytes ends a lock: used bytes of the room were written
 * and are added to the size. KVP_E_ARGUMENT when used is more than the
 * room the buffer has. */
int kvp_buffer_unlock_bytes(kvp_buffer *b, size_t used);
/* kvp_buffer_append copies n bytes at the end, growing as needed.
 * KVP_E_FULL when there is no memory: the buffer stays as it was. */
int kvp_buffer_append(kvp_buffer *b, const void *bytes, size_t n);
/* kvp_buffer_getptr_at gives a pointer to the byte at offset and, in *max,
 * how many bytes in use there are from it on (size - offset): never past
 * size. NULL, and a *max of 0, when offset lies past the size in use or
 * the buffer holds nothing. max may be NULL. Nothing is copied: the pointer
 * is into the buffer and stops being valid when it grows. */
uint8_t *kvp_buffer_getptr_at(const kvp_buffer *b, size_t offset, size_t *max);
/* kvp_buffer_free frees the bytes and leaves the buffer empty, ready to be
 * used again (gr keeps the value it had reached). */
void kvp_buffer_free(kvp_buffer *b);

#ifdef __cplusplus
}
#endif

#endif /* DYN_BUFFER_H */

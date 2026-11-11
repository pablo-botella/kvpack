/*
 * hbkvpack.c — Harbour wrapper over kvpack.c: the writer and the reader
 * as Harbour functions. Plain C against the Harbour API (hbapi.h); build it
 * together with kvpack.c, see hbkvpack.hbp.
 *
 * Writing:
 *   hMsg := KvpNew( nCommand )                   --> writer handle
 *   KvpAddInt32( hMsg, nKey, nValue )
 *   KvpAddInt64( hMsg, nKey, nValue )
 *   KvpAddTs( hMsg, nKey, nUnixSeconds )
 *   KvpAddText( hMsg, nKey, cUtf8 )
 *   KvpAddBin( hMsg, nKey, cBytes )
 *   KvpAddBool( hMsg, nKey, lValue )
 *   KvpAddFloat64( hMsg, nKey, nValue )
 *   KvpAddDs( hMsg, nKey, dDate )                an empty date is the null ds
 *   KvpAddMsg( hMsg, nKey, hSub )                hSub: a writer (encoded now) or a parsed message
 *   cBytes := KvpEncode( hMsg )                  --> the message, ready to send
 *
 * Reading:
 *   hMsg := KvpParse( cBytes, [@nError] )        --> reader handle, or NIL (nError says why)
 *   KvpCommand( hMsg )                           --> command number
 *   KvpCount( hMsg )                             --> number of items
 *   xVal := KvpGet( hMsg, nKey, [nNth] )         --> value typed by the item: int32/int64/ts -> numeric,
 *                                                     text/bin -> string, msg -> reader handle, bool -> logical,
 *                                                     float64 -> numeric, ds -> date; NIL when absent
 *   aVals := KvpGetAll( hMsg, nKey )             --> every value under nKey, in order (a list)
 *   aItem := KvpItem( hMsg, nIndex )             --> { nKey, nType, xValue } of the nIndex-th item (1-based), or NIL
 *   KvpType( hMsg, nKey )                        --> KVP_* type of the first item with nKey, 0 when absent
 *   cText := KvpErrorText( hMsg )                --> text of an error reply (key 0), or NIL
 *   KvpErrorStr( nError )                        --> text of an error code
 *
 * Handles are released by the garbage collector; nothing to close. A
 * handle returned by KvpGet for a nested message owns its own copy of the
 * bytes, so parent and child live independently. Text values are UTF-8
 * bytes in and out; converting to the program's code page is the caller's.
 */
#include "hbapi.h"
#include "hbapiitm.h"
#include "hbapierr.h"

#include "kvpack.h"

#include <string.h>

#define KIND_WRITER 1
#define KIND_READER 2

typedef struct {
    uint16_t key;
    uint16_t type;
    uint32_t size;
    uint8_t *data;
} witem;

/* The handle: a writer keeps its items until KvpEncode; a reader keeps a
 * copy of the bytes and the parsed message over them. */
typedef struct {
    int          kind;
    uint32_t     command;
    witem       *items;
    int          nitems, cap;
    size_t       value_bytes;
    uint8_t     *bytes;
    size_t       len;
    kvp_message msg;
} hb_kvp;

static void free_handle(hb_kvp *h)
{
    int i;
    if (h->items) {
        for (i = 0; i < h->nitems; i++)
            if (h->items[i].data)
                hb_xfree(h->items[i].data);
        hb_xfree(h->items);
        h->items = NULL;
    }
    if (h->bytes) {
        hb_xfree(h->bytes);
        h->bytes = NULL;
    }
    h->nitems = 0;
    h->kind = 0;
}

static HB_GARBAGE_FUNC(hb_kvp_destructor)
{
    free_handle((hb_kvp *)Cargo);
}

static const HB_GC_FUNCS s_gcKvp = { hb_kvp_destructor, hb_gcDummyMark };

static hb_kvp *get_handle(int iParam, int kind)
{
    hb_kvp *h = (hb_kvp *)hb_parptrGC(&s_gcKvp, iParam);
    if (!h || h->kind != kind) {
        hb_errRT_BASE(EG_ARG, 3012, kind == KIND_WRITER ? "not a kvpack writer" : "not a kvpack message",
                      HB_ERR_FUNCNAME, HB_ERR_ARGS_BASEPARAMS);
        return NULL;
    }
    return h;
}

static hb_kvp *new_handle(int kind)
{
    hb_kvp *h = (hb_kvp *)hb_gcAllocate(sizeof(hb_kvp), &s_gcKvp);
    memset(h, 0, sizeof(*h));
    h->kind = kind;
    return h;
}

/* --- writer -------------------------------------------------------------- */

HB_FUNC(KVPNEW)
{
    hb_kvp *h = new_handle(KIND_WRITER);
    h->command = (uint32_t)hb_parnl(1);
    hb_retptrGC(h);
}

/* add_item copies a value into the writer's list; the type's fixed size is
 * checked here so a bad call fails at once, not at KvpEncode. */
static void add_item(hb_kvp *h, uint16_t key, uint16_t type, const void *data, size_t size)
{
    witem *it;
    if (h->nitems == h->cap) {
        int cap = h->cap ? h->cap * 2 : 8;
        h->items = (witem *)(h->items ? hb_xrealloc(h->items, (size_t)cap * sizeof(witem))
                                      : hb_xgrab((size_t)cap * sizeof(witem)));
        h->cap = cap;
    }
    it = &h->items[h->nitems++];
    it->key = key;
    it->type = type;
    it->size = (uint32_t)size;
    it->data = size ? (uint8_t *)hb_xgrab(size) : NULL;
    if (size)
        memcpy(it->data, data, size);
    h->value_bytes += size;
}

static void put64(uint8_t *b, HB_MAXINT v)
{
    int i;
    for (i = 0; i < 8; i++)
        b[i] = (uint8_t)((uint64_t)v >> (8 * i));
}

HB_FUNC(KVPADDINT32)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    uint8_t b[4];
    HB_MAXINT v;
    if (!h)
        return;
    v = hb_parnint(3);
    b[0] = (uint8_t)v; b[1] = (uint8_t)(v >> 8); b[2] = (uint8_t)(v >> 16); b[3] = (uint8_t)(v >> 24);
    add_item(h, (uint16_t)hb_parni(2), KVP_INT32, b, 4);
}

HB_FUNC(KVPADDINT64)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    uint8_t b[8];
    if (!h)
        return;
    put64(b, hb_parnint(3));
    add_item(h, (uint16_t)hb_parni(2), KVP_INT64, b, 8);
}

HB_FUNC(KVPADDTS)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    uint8_t b[8];
    if (!h)
        return;
    put64(b, hb_parnint(3));
    add_item(h, (uint16_t)hb_parni(2), KVP_TS, b, 8);
}

HB_FUNC(KVPADDTEXT)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    if (!h)
        return;
    add_item(h, (uint16_t)hb_parni(2), KVP_TEXT, hb_parcx(3), hb_parclen(3));
}

HB_FUNC(KVPADDBIN)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    if (!h)
        return;
    add_item(h, (uint16_t)hb_parni(2), KVP_BIN, hb_parcx(3), hb_parclen(3));
}

HB_FUNC(KVPADDBOOL)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    uint8_t b;
    if (!h)
        return;
    b = (uint8_t)(hb_parl(3) ? 1 : 0);
    add_item(h, (uint16_t)hb_parni(2), KVP_BOOL, &b, 1);
}

HB_FUNC(KVPADDFLOAT64)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    double d;
    uint64_t bits;
    uint8_t b[8];
    if (!h)
        return;
    d = hb_parnd(3);
    memcpy(&bits, &d, 8);
    put64(b, (HB_MAXINT)bits);
    add_item(h, (uint16_t)hb_parni(2), KVP_FLOAT64, b, 8);
}

/* KvpAddDs takes a Harbour date and writes what DToS gives: yyyymmdd, or 8
 * spaces (the null ds) for an empty date. */
HB_FUNC(KVPADDDS)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    char s[9];
    if (!h)
        return;
    hb_pardsbuff(s, 3);
    add_item(h, (uint16_t)hb_parni(2), KVP_DS, s, 8);
}

/* encode builds the message of a writer into a fresh buffer (one spare
 * byte for the terminator hb_retclen_buffer writes); the caller frees it
 * with hb_xfree or hands it to Harbour. */
static uint8_t *encode(const hb_kvp *h, size_t *len)
{
    size_t room = kvp_room((uint32_t)h->nitems, h->value_bytes);
    uint8_t *buf = (uint8_t *)hb_xgrab(room + 1);
    kvp_writer w;
    int i;
    kvp_begin(&w, buf, room, h->command);
    for (i = 0; i < h->nitems; i++)
        kvp_add(&w, h->items[i].key, h->items[i].type, h->items[i].data, h->items[i].size);
    if (kvp_end(&w, len) != KVP_OK) {
        hb_xfree(buf);
        return NULL;
    }
    return buf;
}

HB_FUNC(KVPADDMSG)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    hb_kvp *sub = (hb_kvp *)hb_parptrGC(&s_gcKvp, 3);
    if (!h)
        return;
    if (!sub || !sub->kind) {
        hb_errRT_BASE(EG_ARG, 3012, "not a kvpack message", HB_ERR_FUNCNAME, HB_ERR_ARGS_BASEPARAMS);
        return;
    }
    if (sub->kind == KIND_READER) {
        add_item(h, (uint16_t)hb_parni(2), KVP_MSG, sub->bytes, sub->len);
    } else {
        size_t len;
        uint8_t *buf = encode(sub, &len);
        if (!buf) {
            hb_errRT_BASE(EG_ARG, 3012, "cannot encode the nested message", HB_ERR_FUNCNAME, HB_ERR_ARGS_BASEPARAMS);
            return;
        }
        add_item(h, (uint16_t)hb_parni(2), KVP_MSG, buf, len);
        hb_xfree(buf);
    }
}

HB_FUNC(KVPENCODE)
{
    hb_kvp *h = get_handle(1, KIND_WRITER);
    size_t len;
    uint8_t *buf;
    if (!h)
        return;
    buf = encode(h, &len);
    if (!buf) {
        hb_ret();
        return;
    }
    hb_retclen_buffer((char *)buf, (HB_SIZE)len);
}

/* --- reader -------------------------------------------------------------- */

/* parse_into makes a reader over a copy of len bytes; returns the error. */
static int parse_into(hb_kvp *h, const void *bytes, size_t len)
{
    int rc;
    h->bytes = (uint8_t *)hb_xgrab(len ? len : 1);
    memcpy(h->bytes, bytes, len);
    h->len = len;
    rc = kvp_parse(h->bytes, len, &h->msg);
    if (rc != KVP_OK) {
        hb_xfree(h->bytes);
        h->bytes = NULL;
        h->len = 0;
    }
    return rc;
}

HB_FUNC(KVPPARSE)
{
    hb_kvp *h = new_handle(KIND_READER);
    int rc = parse_into(h, hb_parcx(1), hb_parclen(1));
    hb_storni(rc, 2);
    if (rc != KVP_OK) {
        hb_gcFree(h);
        hb_ret();
        return;
    }
    h->command = h->msg.command;
    hb_retptrGC(h);
}

HB_FUNC(KVPCOMMAND)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    if (h)
        hb_retnl((long)h->msg.command);
}

HB_FUNC(KVPCOUNT)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    if (h)
        hb_retnl((long)h->msg.nitems);
}

static HB_MAXINT get64(const uint8_t *b)
{
    uint64_t v = 0;
    int i;
    for (i = 7; i >= 0; i--)
        v = (v << 8) | b[i];
    return (HB_MAXINT)v;
}

/* item_value builds the Harbour value of an item: numeric for the integer
 * types, string for text and bin, a new reader handle for a nested message. */
static PHB_ITEM item_value(const kvp_item *it)
{
    PHB_ITEM v = hb_itemNew(NULL);
    switch (it->type) {
    case KVP_INT32:
        hb_itemPutNL(v, (long)(int32_t)((uint32_t)it->value[0] | ((uint32_t)it->value[1] << 8)
                                        | ((uint32_t)it->value[2] << 16) | ((uint32_t)it->value[3] << 24)));
        break;
    case KVP_INT64:
    case KVP_TS:
        hb_itemPutNInt(v, get64(it->value));
        break;
    case KVP_TEXT:
    case KVP_BIN:
        hb_itemPutCL(v, (const char *)it->value, it->size);
        break;
    case KVP_BOOL:
        hb_itemPutL(v, it->value[0] != 0);
        break;
    case KVP_FLOAT64: {
        double d;
        uint64_t bits = (uint64_t)get64(it->value);
        memcpy(&d, &bits, 8);
        hb_itemPutND(v, d);
        break;
    }
    case KVP_DS: {
        int32_t ymd = 0;                             /* 0: null or malformed, an empty date */
        kvp_ds(it, &ymd);
        if (ymd == 0)
            hb_itemPutDL(v, 0);
        else
            hb_itemPutD(v, ymd / 10000, ymd / 100 % 100, ymd % 100);
        break;
    }
    case KVP_MSG: {
        hb_kvp *sub = new_handle(KIND_READER);
        if (parse_into(sub, it->value, it->size) != KVP_OK) {
            hb_gcFree(sub);
            break;                                   /* NIL: cannot happen after the parent's parse */
        }
        sub->command = sub->msg.command;
        hb_itemPutPtrGC(v, sub);
        break;
    }
    }
    return v;
}

HB_FUNC(KVPGET)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    kvp_item it;
    int nth = hb_parnidef(3, 1), rc;
    if (!h)
        return;
    for (rc = kvp_find(&h->msg, (uint16_t)hb_parni(2), &it); rc == KVP_OK && --nth > 0; rc = kvp_find_next(&h->msg, &it))
        ;
    if (rc != KVP_OK) {
        hb_ret();
        return;
    }
    hb_itemReturnRelease(item_value(&it));
}

HB_FUNC(KVPGETALL)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    PHB_ITEM a;
    kvp_item it;
    int rc;
    if (!h)
        return;
    a = hb_itemArrayNew(0);
    for (rc = kvp_find(&h->msg, (uint16_t)hb_parni(2), &it); rc == KVP_OK; rc = kvp_find_next(&h->msg, &it)) {
        PHB_ITEM v = item_value(&it);
        hb_arrayAdd(a, v);
        hb_itemRelease(v);
    }
    hb_itemReturnRelease(a);
}

HB_FUNC(KVPITEM)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    kvp_item it;
    int n = hb_parni(2), rc;
    PHB_ITEM a, v;
    if (!h)
        return;
    if (n < 1) {
        hb_ret();
        return;
    }
    for (rc = kvp_first(&h->msg, &it); rc == KVP_OK && --n > 0; rc = kvp_next(&h->msg, &it))
        ;
    if (rc != KVP_OK) {
        hb_ret();
        return;
    }
    a = hb_itemArrayNew(3);
    hb_arraySetNI(a, 1, it.key);
    hb_arraySetNI(a, 2, it.type);
    v = item_value(&it);
    hb_arraySet(a, 3, v);
    hb_itemRelease(v);
    hb_itemReturnRelease(a);
}

HB_FUNC(KVPTYPE)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    kvp_item it;
    if (!h)
        return;
    hb_retni(kvp_find(&h->msg, (uint16_t)hb_parni(2), &it) == KVP_OK ? it.type : 0);
}

HB_FUNC(KVPERRORTEXT)
{
    hb_kvp *h = get_handle(1, KIND_READER);
    kvp_item it;
    if (!h)
        return;
    if (kvp_find(&h->msg, KVP_KEY_ERROR, &it) == KVP_OK && it.type == KVP_TEXT)
        hb_retclen((const char *)it.value, it.size);
    else
        hb_ret();
}

/* --- misc ---------------------------------------------------------------- */

HB_FUNC(KVPERRORSTR)
{
    hb_retc(kvp_error_str(hb_parni(1)));
}

HB_FUNC(KVPTYPESTR)
{
    hb_retc(kvp_type_str((uint16_t)hb_parni(1)));
}

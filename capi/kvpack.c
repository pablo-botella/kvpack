/*
 * kvpack.c — writer and reader for kvpack messages. See kvpack.h.
 */
#include "kvpack.h"

#include <string.h>

/* --- little-endian helpers ----------------------------------------------- */

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

static void wr64(uint8_t *p, uint64_t v)
{
    wr32(p, (uint32_t)v);
    wr32(p + 4, (uint32_t)(v >> 32));
}

static uint16_t rd16(const uint8_t *p)
{
    return (uint16_t)(p[0] | (p[1] << 8));
}

static uint32_t rd32(const uint8_t *p)
{
    return (uint32_t)p[0] | ((uint32_t)p[1] << 8) | ((uint32_t)p[2] << 16) | ((uint32_t)p[3] << 24);
}

static uint64_t rd64(const uint8_t *p)
{
    return (uint64_t)rd32(p) | ((uint64_t)rd32(p + 4) << 32);
}

/* --- crc32 (IEEE) --------------------------------------------------------- */

static uint32_t crc_table[256];
static int crc_ready;

static void crc_init(void)
{
    uint32_t i, j, c;
    for (i = 0; i < 256; i++) {
        c = i;
        for (j = 0; j < 8; j++)
            c = (c & 1) ? 0xEDB88320u ^ (c >> 1) : c >> 1;
        crc_table[i] = c;
    }
    crc_ready = 1;
}

uint32_t kvp_crc32(uint32_t crc, const void *data, size_t len)
{
    const uint8_t *p = (const uint8_t *)data;
    if (!crc_ready)
        crc_init();
    crc = ~crc;
    while (len--)
        crc = crc_table[(crc ^ *p++) & 0xFF] ^ (crc >> 8);
    return ~crc;
}

/* --- types --------------------------------------------------------------- */

/* fixed_size is the value size a type demands; 0 when any size is valid;
 * -1 for an unknown type. */
static int fixed_size(uint16_t type)
{
    switch (type) {
    case KVP_INT32: return 4;
    case KVP_INT64: return 8;
    case KVP_TS:    return 8;
    case KVP_TEXT:  return 0;
    case KVP_BIN:   return 0;
    case KVP_MSG:   return 0;
    case KVP_BOOL:  return 1;
    case KVP_FLOAT64: return 8;
    case KVP_DS:    return 8;
    }
    return -1;
}

static int days_in_month(int32_t y, int32_t m)
{
    static const int days[12] = { 31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31 };
    if (m == 2 && ((y % 4 == 0 && y % 100 != 0) || y % 400 == 0))
        return 29;
    return days[m - 1];
}

/* ds_valid says whether yyyymmdd is a calendar day of the years 1..9999. */
static int ds_valid(int32_t ymd)
{
    int32_t y = ymd / 10000, m = ymd / 100 % 100, d = ymd % 100;
    return ymd > 0 && y >= 1 && y <= 9999 && m >= 1 && m <= 12 && d >= 1 && d <= days_in_month(y, m);
}

/* ds_read reads a ds value as Harbour's SToD does: yyyymmdd for 8 digits
 * of a real day, 0 (the null date) for anything else -- 8 spaces,
 * "00000000", a 30th of February. */
static int32_t ds_read(const uint8_t *v)
{
    int32_t n = 0;
    int i;
    for (i = 0; i < 8; i++) {
        if (v[i] < '0' || v[i] > '9')
            return 0;
        n = n * 10 + (v[i] - '0');
    }
    return ds_valid(n) ? n : 0;
}

/* valid_value checks what a fixed size alone does not: a bool is 0 or 1.
 * A ds is never refused: what is not a real day reads as the null date. */
static int valid_value(uint16_t type, const uint8_t *v)
{
    if (type == KVP_BOOL)
        return v[0] <= 1;
    return 1;
}

/* --- writing ------------------------------------------------------------- */

size_t kvp_room(uint32_t nitems, size_t value_bytes)
{
    return KVP_HEADER_SIZE + (size_t)nitems * KVP_ITEM_SIZE + value_bytes;
}

int kvp_begin(kvp_writer *w, void *buf, size_t cap, uint32_t command)
{
    if (!w)
        return KVP_E_ARGUMENT;
    memset(w, 0, sizeof *w);
    if (!buf || cap < KVP_HEADER_SIZE) {
        w->error = buf ? KVP_E_FULL : KVP_E_ARGUMENT;
        return w->error;
    }
    w->buf = (uint8_t *)buf;
    w->cap = cap;
    w->len = KVP_HEADER_SIZE;
    w->buf[0] = 'K';
    w->buf[1] = 'V';
    w->buf[2] = 'P';
    w->buf[3] = KVP_VERSION;
    wr32(w->buf + 4, command);
    wr32(w->buf + 8, 0);
    wr32(w->buf + 12, 0);
    wr32(w->buf + 16, 0);
    return KVP_OK;
}

static int fail(kvp_writer *w, int error)
{
    if (w->error == KVP_OK)
        w->error = error;
    return error;
}

int kvp_add(kvp_writer *w, uint16_t key, uint16_t type, const void *value, uint32_t size)
{
    int need;
    uint8_t *p;
    if (!w)
        return KVP_E_ARGUMENT;
    if (w->error)
        return w->error;
    if (!value && size)
        return fail(w, KVP_E_ARGUMENT);
    need = fixed_size(type);
    if (need < 0 || (need > 0 && (uint32_t)need != size))
        return fail(w, KVP_E_TYPE);
    if (need > 0 && !valid_value(type, (const uint8_t *)value))
        return fail(w, KVP_E_ARGUMENT);
    if (w->cap - w->len < KVP_ITEM_SIZE + (size_t)size)
        return fail(w, KVP_E_FULL);
    p = w->buf + w->len;
    wr16(p, key);
    wr16(p + 2, type);
    wr32(p + 4, size);
    if (size)
        memcpy(p + KVP_ITEM_SIZE, value, size);
    w->len += KVP_ITEM_SIZE + size;
    w->nitems++;
    return KVP_OK;
}

int kvp_add_int32(kvp_writer *w, uint16_t key, int32_t v)
{
    uint8_t b[4];
    wr32(b, (uint32_t)v);
    return kvp_add(w, key, KVP_INT32, b, 4);
}

int kvp_add_int64(kvp_writer *w, uint16_t key, int64_t v)
{
    uint8_t b[8];
    wr64(b, (uint64_t)v);
    return kvp_add(w, key, KVP_INT64, b, 8);
}

int kvp_add_ts(kvp_writer *w, uint16_t key, int64_t unix_seconds)
{
    uint8_t b[8];
    wr64(b, (uint64_t)unix_seconds);
    return kvp_add(w, key, KVP_TS, b, 8);
}

int kvp_add_text(kvp_writer *w, uint16_t key, const char *utf8)
{
    size_t n;
    if (!utf8)
        return w ? fail(w, KVP_E_ARGUMENT) : KVP_E_ARGUMENT;
    n = strlen(utf8);
    if (n > 0xFFFFFFFFu)
        return fail(w, KVP_E_FULL);
    return kvp_add(w, key, KVP_TEXT, utf8, (uint32_t)n);
}

int kvp_add_bin(kvp_writer *w, uint16_t key, const void *bytes, uint32_t size)
{
    return kvp_add(w, key, KVP_BIN, bytes, size);
}

int kvp_add_msg(kvp_writer *w, uint16_t key, const void *msg, size_t len)
{
    if (len > 0xFFFFFFFFu)
        return w ? fail(w, KVP_E_FULL) : KVP_E_ARGUMENT;
    return kvp_add(w, key, KVP_MSG, msg, (uint32_t)len);
}

int kvp_add_bool(kvp_writer *w, uint16_t key, int v)
{
    uint8_t b = (uint8_t)(v != 0);
    return kvp_add(w, key, KVP_BOOL, &b, 1);
}

int kvp_add_float64(kvp_writer *w, uint16_t key, double v)
{
    uint64_t bits;
    uint8_t b[8];
    memcpy(&bits, &v, 8);           /* IEEE 754 double, as on every target */
    wr64(b, bits);
    return kvp_add(w, key, KVP_FLOAT64, b, 8);
}

int kvp_add_ds(kvp_writer *w, uint16_t key, int32_t yyyymmdd)
{
    uint8_t b[8];
    int i;
    if (!ds_valid(yyyymmdd)) {
        memset(b, ' ', 8);                    /* 0, or no real day: the null date */
    } else {
        for (i = 7; i >= 0; i--) {
            b[i] = (uint8_t)('0' + yyyymmdd % 10);
            yyyymmdd /= 10;
        }
    }
    return kvp_add(w, key, KVP_DS, b, 8);
}

kvp_utf8 kvp_utf8_of(const char *s)
{
    kvp_utf8 t;
    t.p = s;
    t.len = s ? (uint32_t)strlen(s) : 0;
    return t;
}

int kvp_add_utf8(kvp_writer *w, uint16_t key, const kvp_utf8 *t)
{
    if (!t)
        return w ? fail(w, KVP_E_ARGUMENT) : KVP_E_ARGUMENT;
    return kvp_add(w, key, KVP_TEXT, t->p, t->len);
}

int kvp_begin_msg(kvp_writer *w, uint16_t key, kvp_writer *sub, uint32_t command)
{
    uint8_t *p;
    if (!w || !sub)
        return KVP_E_ARGUMENT;
    memset(sub, 0, sizeof *sub);
    if (w->error) {
        sub->error = w->error;
        return w->error;
    }
    if (w->cap - w->len < KVP_ITEM_SIZE + KVP_HEADER_SIZE) {
        sub->error = fail(w, KVP_E_FULL);
        return sub->error;
    }
    p = w->buf + w->len;
    wr16(p, key);
    wr16(p + 2, KVP_MSG);
    wr32(p + 4, 0);                 /* sealed by kvp_end_msg */
    return kvp_begin(sub, p + KVP_ITEM_SIZE, w->cap - w->len - KVP_ITEM_SIZE, command);
}

int kvp_end_msg(kvp_writer *w, kvp_writer *sub)
{
    size_t n;
    int rc;
    if (!w || !sub)
        return KVP_E_ARGUMENT;
    if (w->error)
        return w->error;
    rc = kvp_end(sub, &n);
    if (rc != KVP_OK)
        return fail(w, rc);
    wr32(w->buf + w->len + 4, (uint32_t)n);
    w->len += KVP_ITEM_SIZE + n;
    w->nitems++;
    return KVP_OK;
}

int kvp_end(kvp_writer *w, size_t *len)
{
    uint32_t size;
    if (!w)
        return KVP_E_ARGUMENT;
    if (w->error)
        return w->error;
    size = (uint32_t)(w->len - KVP_HEADER_SIZE);
    wr32(w->buf + 8, w->nitems);
    wr32(w->buf + 12, size);
    wr32(w->buf + 16, kvp_crc32(0, w->buf + KVP_HEADER_SIZE, size));
    if (len)
        *len = w->len;
    return KVP_OK;
}

/* --- reading ------------------------------------------------------------- */

int kvp_header(const void *buf, size_t len, kvp_message *m)
{
    const uint8_t *p = (const uint8_t *)buf;
    if (!buf || !m)
        return KVP_E_ARGUMENT;
    memset(m, 0, sizeof *m);
    if (len < KVP_HEADER_SIZE)
        return KVP_E_TRUNCATED;
    if (p[0] != 'K' || p[1] != 'V' || p[2] != 'P')
        return KVP_E_MAGIC;
    if (p[3] != KVP_VERSION)
        return KVP_E_VERSION;
    m->buf = p;
    m->command = rd32(p + 4);
    m->nitems = rd32(p + 8);
    m->size = rd32(p + 12);
    m->crc32 = rd32(p + 16);
    m->len = KVP_HEADER_SIZE + (size_t)m->size;
    return KVP_OK;
}

/* item_at reads the item header at data offset pos and checks it lies
 * inside the data block. */
static int item_at(const kvp_message *m, size_t pos, kvp_item *it)
{
    const uint8_t *data = m->buf + KVP_HEADER_SIZE;
    int need;
    if (m->size - pos < KVP_ITEM_SIZE)
        return KVP_E_FORMAT;
    it->key = rd16(data + pos);
    it->type = rd16(data + pos + 2);
    it->size = rd32(data + pos + 4);
    it->value = data + pos + KVP_ITEM_SIZE;
    it->pos = pos;
    if (it->size > m->size - pos - KVP_ITEM_SIZE)
        return KVP_E_FORMAT;
    need = fixed_size(it->type);
    if (need < 0 || (need > 0 && (uint32_t)need != it->size))
        return KVP_E_FORMAT;
    if (need > 0 && !valid_value(it->type, it->value))
        return KVP_E_FORMAT;
    return KVP_OK;
}

int kvp_parse(const void *buf, size_t len, kvp_message *m)
{
    kvp_item it;
    size_t pos = 0;
    uint32_t i;
    int rc = kvp_header(buf, len, m);
    if (rc != KVP_OK)
        return rc;
    if (len < m->len)
        return KVP_E_TRUNCATED;
    if (len != m->len)
        return KVP_E_FORMAT;
    if ((uint64_t)m->nitems * KVP_ITEM_SIZE > m->size)
        return KVP_E_FORMAT;
    if (kvp_crc32(0, m->buf + KVP_HEADER_SIZE, m->size) != m->crc32)
        return KVP_E_CRC;
    for (i = 0; i < m->nitems; i++) {
        rc = item_at(m, pos, &it);
        if (rc != KVP_OK)
            return rc;
        if (it.type == KVP_MSG) {
            kvp_message sub;
            if (kvp_parse(it.value, it.size, &sub) != KVP_OK)
                return KVP_E_FORMAT;
        }
        pos += KVP_ITEM_SIZE + it.size;
    }
    if (pos != m->size)
        return KVP_E_FORMAT;
    return KVP_OK;
}

int kvp_first(const kvp_message *m, kvp_item *it)
{
    if (!m || !it || !m->buf)
        return KVP_E_ARGUMENT;
    if (m->nitems == 0)
        return KVP_E_NOT_FOUND;
    return item_at(m, 0, it);
}

int kvp_next(const kvp_message *m, kvp_item *it)
{
    size_t pos;
    if (!m || !it || !m->buf)
        return KVP_E_ARGUMENT;
    pos = it->pos + KVP_ITEM_SIZE + it->size;
    if (pos >= m->size)
        return KVP_E_NOT_FOUND;
    return item_at(m, pos, it);
}

int kvp_find(const kvp_message *m, uint16_t key, kvp_item *it)
{
    int rc;
    for (rc = kvp_first(m, it); rc == KVP_OK; rc = kvp_next(m, it))
        if (it->key == key)
            return KVP_OK;
    return rc;
}

int kvp_find_next(const kvp_message *m, kvp_item *it)
{
    uint16_t key;
    int rc;
    if (!it)
        return KVP_E_ARGUMENT;
    key = it->key;
    for (rc = kvp_next(m, it); rc == KVP_OK; rc = kvp_next(m, it))
        if (it->key == key)
            return KVP_OK;
    return rc;
}

/* --- typed access -------------------------------------------------------- */

int kvp_int32(const kvp_item *it, int32_t *v)
{
    if (!it || !v)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_INT32)
        return KVP_E_TYPE;
    *v = (int32_t)rd32(it->value);
    return KVP_OK;
}

int kvp_int64(const kvp_item *it, int64_t *v)
{
    if (!it || !v)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_INT64)
        return KVP_E_TYPE;
    *v = (int64_t)rd64(it->value);
    return KVP_OK;
}

int kvp_ts(const kvp_item *it, int64_t *unix_seconds)
{
    if (!it || !unix_seconds)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_TS)
        return KVP_E_TYPE;
    *unix_seconds = (int64_t)rd64(it->value);
    return KVP_OK;
}

int kvp_text(const kvp_item *it, char *dst, size_t cap)
{
    size_t n;
    if (!it || !dst || cap == 0)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_TEXT)
        return KVP_E_TYPE;
    n = it->size < cap - 1 ? it->size : cap - 1;
    memcpy(dst, it->value, n);
    dst[n] = 0;
    return n == it->size ? KVP_OK : KVP_E_FULL;
}

int kvp_bin(const kvp_item *it, const void **bytes, uint32_t *size)
{
    if (!it || !bytes || !size)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_BIN)
        return KVP_E_TYPE;
    *bytes = it->value;
    *size = it->size;
    return KVP_OK;
}

int kvp_msg(const kvp_item *it, kvp_message *sub)
{
    if (!it || !sub)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_MSG)
        return KVP_E_TYPE;
    return kvp_parse(it->value, it->size, sub);
}

int kvp_item_utf8(const kvp_item *it, kvp_utf8 *t)
{
    if (!it || !t)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_TEXT)
        return KVP_E_TYPE;
    t->p = (const char *)it->value;
    t->len = it->size;
    return KVP_OK;
}

int kvp_bool(const kvp_item *it, int *v)
{
    if (!it || !v)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_BOOL)
        return KVP_E_TYPE;
    *v = it->value[0];
    return KVP_OK;
}

int kvp_float64(const kvp_item *it, double *v)
{
    uint64_t bits;
    if (!it || !v)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_FLOAT64)
        return KVP_E_TYPE;
    bits = rd64(it->value);
    memcpy(v, &bits, 8);
    return KVP_OK;
}

int kvp_ds(const kvp_item *it, int32_t *yyyymmdd)
{
    if (!it || !yyyymmdd)
        return KVP_E_ARGUMENT;
    if (it->type != KVP_DS)
        return KVP_E_TYPE;
    *yyyymmdd = ds_read(it->value);
    return KVP_OK;
}

int kvp_get_bool(const kvp_message *m, uint16_t key, int *v)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_bool(&it, v) : rc;
}

int kvp_get_float64(const kvp_message *m, uint16_t key, double *v)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_float64(&it, v) : rc;
}

int kvp_get_ds(const kvp_message *m, uint16_t key, int32_t *yyyymmdd)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_ds(&it, yyyymmdd) : rc;
}

int kvp_get_int32(const kvp_message *m, uint16_t key, int32_t *v)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_int32(&it, v) : rc;
}

int kvp_get_int64(const kvp_message *m, uint16_t key, int64_t *v)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_int64(&it, v) : rc;
}

int kvp_get_ts(const kvp_message *m, uint16_t key, int64_t *unix_seconds)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_ts(&it, unix_seconds) : rc;
}

int kvp_get_text(const kvp_message *m, uint16_t key, char *dst, size_t cap)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_text(&it, dst, cap) : rc;
}

int kvp_get_bin(const kvp_message *m, uint16_t key, const void **bytes, uint32_t *size)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_bin(&it, bytes, size) : rc;
}

int kvp_get_msg(const kvp_message *m, uint16_t key, kvp_message *sub)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_msg(&it, sub) : rc;
}

int kvp_get_utf8(const kvp_message *m, uint16_t key, kvp_utf8 *t)
{
    kvp_item it;
    int rc = kvp_find(m, key, &it);
    return rc == KVP_OK ? kvp_item_utf8(&it, t) : rc;
}

int kvp_error_text(const kvp_message *m, char *dst, size_t cap)
{
    return kvp_get_text(m, KVP_KEY_ERROR, dst, cap);
}

int kvp_is_error(const kvp_message *m)
{
    kvp_utf8 t;
    return kvp_get_utf8(m, KVP_KEY_ERROR, &t) == KVP_OK;
}

/* --- misc ---------------------------------------------------------------- */

const char *kvp_error_str(int error)
{
    switch (error) {
    case KVP_OK:          return "ok";
    case KVP_E_ARGUMENT:  return "invalid argument";
    case KVP_E_MAGIC:     return "not a kvpack message";
    case KVP_E_VERSION:   return "unsupported version";
    case KVP_E_TRUNCATED: return "message cut short";
    case KVP_E_FORMAT:    return "inconsistent message";
    case KVP_E_CRC:       return "crc32 mismatch";
    case KVP_E_FULL:      return "buffer too small";
    case KVP_E_NOT_FOUND: return "not found";
    case KVP_E_TYPE:      return "wrong type";
    case KVP_E_MISSING:   return "required field missing";
    case KVP_E_REPLY:     return "error reply";
    }
    return "unknown error";
}

const char *kvp_type_str(uint16_t type)
{
    switch (type) {
    case KVP_INT32: return "int32";
    case KVP_INT64: return "int64";
    case KVP_TS:    return "ts";
    case KVP_TEXT:  return "text";
    case KVP_BIN:   return "bin";
    case KVP_MSG:   return "msg";
    case KVP_BOOL:  return "bool";
    case KVP_FLOAT64: return "float64";
    case KVP_DS:    return "ds";
    }
    return "?";
}

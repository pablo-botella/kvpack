# kvpack/capi

Writer and reader for [kvpack](https://github.com/pablo-botella/kvpack)
messages in plain C, plus the same as Harbour functions. ANSI C (C89) with
64-bit integers, nothing beyond the C standard library, no allocation: the
writer fills a buffer the caller provides and the reader points into the
bytes the caller received. Builds with MSVC, MinGW, gcc or clang, 32 or 64
bits; the two source files drop into any program.

- `kvpack.h` / `kvpack.c` — the writer and the reader (the format is
  fully described in the header).
- `test_kvpack.c` — the C test: the canonical message shared with the Go
  and Harbour tests, lists, nested messages, limits and every rejection.
- `dyn_buffer.h` / `dyn_buffer.c` and `kvp_arena.h` / `kvp_arena.c` —
  optional, for a message whose size is not known in advance: a buffer
  that grows through the application's `malloc` and `free`, and messages
  as fixed structs over it (see [Growing messages](#growing-messages)).
  `kvpack.c` needs neither. `test_dyn_buffer.c` is the buffer's test; the
  arena is exercised by the test of the generated code.
- `hbkvpack.c` / `kvpack.ch` / `hbkvpack.hbp` — the Harbour wrapper
  (see below) and `test_hbkvpack.prg` / `.hbp`, its test.
- `kvpclass.prg` — the Harbour classes over the wrapper: `KvpTupleSet`
  and `KvpStruct`, the base of the classes generated from a `.kvp`.

## Build and test

```bat
build.bat          :: MSVC, 32-bit, static CRT; builds and runs test_kvpack and test_dyn_buffer
build.bat x64      :: MSVC, 64-bit
make               :: gcc / MinGW / clang, -std=c89 -pedantic; builds and runs
hbmk2 hbkvpack.hbp        :: the Harbour wrapper and its classes as a static library
hbmk2 test_hbkvpack.hbp   :: the Harbour test, then run test_hbkvpack
```

## C

```c
#include "kvpack.h"

/* write */
uint8_t buf[512];
kvp_writer w;
size_t len;
kvp_begin(&w, buf, sizeof buf, CMD_LOGIN);
kvp_add_text(&w, K_USER, "bob");
kvp_add_text(&w, K_PASSWORD, "secret");
kvp_add_ts(&w, K_WHEN, (int64_t)time(NULL));
if (kvp_end(&w, &len) == KVP_OK)
    send(buf, len);                       /* the message is buf[0..len) */

/* read */
kvp_message m;
int32_t code;
char name[64];
if (kvp_parse(bytes, n, &m) == KVP_OK) {        /* header, items, crc32 all checked */
    if (kvp_error_text(&m, name, sizeof name) == KVP_OK)
        ...                                       /* an error reply */
    kvp_get_int32(&m, K_CODE, &code);
    kvp_get_text(&m, K_NAME, name, sizeof name);
}
```

- **Writer.** `kvp_begin(w, buf, cap, command)`, then `kvp_add_int32`,
  `_int64`, `_ts`, `_text`, `_bin`, `_bool` (nonzero is true), `_float64`,
  `_ds` (an `int32_t` `yyyymmdd`, 0 for the null date; what is no real day
  is written as the null date), `_null` (the key, no value), `_msg` (a message built by another
  writer) or the raw `kvp_add(w, key, type, value, size)`, then
  `kvp_end(w, &len)` seals count, size and crc32. Once an add fails
  (`KVP_E_FULL`, `KVP_E_TYPE`) the writer stays failed and `kvp_end`
  reports it, so a run of adds is checked once. `kvp_room(nitems,
  value_bytes)` sizes a buffer.
- **Reader.** `kvp_parse(buf, len, &m)` validates everything (nested
  messages included) and `m` points into `buf`, which must outlive it. The
  walk is `kvp_first` / `kvp_next`, or `kvp_find(key)` / `kvp_find_next`
  for a repeated key; an item is read with `kvp_int32`, `_int64`, `_ts`,
  `_text` (copied, NUL-terminated, `KVP_E_FULL` when truncated), `_bin`
  (pointer and size into the message), `_bool` (0/1), `_float64`, `_ds`
  (`yyyymmdd`, 0 for the null date) or `_msg` (a nested `kvp_message`).
  The `kvp_get_*` family does find and read in one call, first item wins.
- **Streams.** `kvp_header(buf, 20, &m)` reads only the header so a
  socket reader knows how many bytes follow (`m.size`) before `kvp_parse`.
- **By reference.** `kvp_utf8` and `kvp_bytes` are `{ p, len }` pairs:
  `kvp_get_utf8` / `kvp_item_utf8` point one at a text item with no copy
  and no terminator (convert straight from `p, len`), `kvp_add_utf8` writes
  one, `kvp_utf8_of("...")` makes one from a C string.
- **Nesting in place.** `kvp_begin_msg(w, key, &sub, command)` opens a
  nested message inside `w`'s own buffer; add to `sub`, then
  `kvp_end_msg(w, &sub)`. No second buffer, no copy.
- **Null.** `kvp_is_null(&m, key)` says 1 when the first item with that
  key is a null (0 when it holds a value or is absent); in a walk a null is
  an item whose `type` is `KVP_NULL`. The typed readers give `KVP_E_TYPE`
  on it, as on any other type.
- **Tuples.** For data whose shape is only known at run time: a
  definition, then its tuples, all under one key and in that order.
  Writing: `kvp_begin_tupledef(w, key, &def)`, one
  `kvp_tupledef_add(&def, type, name, meta, meta_size)` per field (type
  `KVP_INT32`..`KVP_DS`; the meta is the application's, raw bytes and
  their size: `NULL, 0` for none, or `"text", KVP_STRLEN` for one that is a
  NUL-terminated string, measured for you),
  `kvp_end_tupledef(w, &def)`; then per tuple
  `kvp_begin_tuple(w, key, &row)`, one `kvp_tuple_int32`, `_int64`, `_ts`,
  `_text`, `_utf8`, `_bin`, `_msg`, `_bool`, `_float64`, `_ds` or `_null`
  per field in the order of the definition, and `kvp_end_tuple(w, &row)`.
  The writer refuses what a reader would: a second definition under a key,
  a tuple that does not follow its definition, a value of another type
  than its field, fewer or more values than fields. Reading:
  `kvp_get_tupledef(&m, key, &def)`, `kvp_tupledef_field(&def, i, &f)` for
  the type, name and meta of each field, then
  `for (row = def.item; kvp_tuple_next(&m, &row) == KVP_OK; )` and
  `kvp_tuple_field(&def, &row, i, &item)`, which gives the field as an item
  to read with `kvp_int32`, `kvp_item_utf8`... — its type is `KVP_NULL`
  when the field is null. Nothing is allocated or copied.
- `kvp_is_error(&m)` says whether a reply is an error reply.
- Every function returns `KVP_OK` (0) or a negative `kvp_error`;
  `kvp_error_str` names it. `KVP_E_MISSING` and `KVP_E_REPLY` are returned
  by code generated from a `.kvp` (see the main README).

## Growing messages

The writer above needs a buffer big enough before it starts, and nothing
in C knows the size of a message before it is written: not the library,
which sees items go by, nor the code generated from a `.kvp`, which knows
the fields but not the data — the rows of a query, say. Two optional files
close that gap without making `kvpack.c` allocate.

**`dyn_buffer.h`** — a buffer that grows, over two functions with the
signatures of `malloc` and `free`, the C library's or the application's
own:

```c
kvp_buffer b;
kvp_buffer_init(&b, malloc, free, 0, 0);     /* granularity and its limit; 0, 0: 4096 doubling up to 1 MB */
p = kvp_buffer_lock_bytes(&b, 512);          /* room for up to 512 bytes, or NULL */
...write n of them at p...
kvp_buffer_unlock_bytes(&b, n);              /* n more bytes in use */
kvp_buffer_append(&b, bytes, n);             /* or copy some at the end */
p = kvp_buffer_getptr_at(&b, 0, &max);       /* what is in use: b.size bytes */
kvp_buffer_free(&b);
```

It grows in steps of `gr` bytes and `gr` doubles each time, up to `grmax`
(`kvp_buffer_set_granularity` changes both at any time). Growing is a new
block, the bytes copied and the old one freed, so `b.data` moves: no
pointer into it survives a lock or an append. `kvp_buffer_wrap(&b, bytes,
n)` points one at bytes the caller already has, to read them as a buffer;
such a buffer never grows and frees nothing.

**`kvp_arena.h`** — messages as fixed structs over such a buffer, used as
an arena. The struct is the caller's and its members are small records: a
scalar is `{ null, value }`; a text, bin or nested message is `{ null,
size, off }`, its bytes copied into the arena and referred to by offset —
never by pointer, because the block moves — and lists and tuples are
chained inside the arena the same way. When everything is in, the exact
size of the message is known, the caller allocates it, and the message is
written in one pass. One `kvp_buffer_free` releases it all.

```c
kvp_buffer a;
api_query_reply r;                               /* generated from a .kvp */
kvp_buffer_init(&a, malloc, free, 0, 0);
memset(&r, 0, sizeof r);
r.code.value = 0;
kvp_set_text(&a, &r.note, "ok");
kvp_tupleset_field(&a, &r.rows, KVP_INT32, "id", NULL, 0);
kvp_tupleset_field(&a, &r.rows, KVP_TEXT, "name", NULL, 0);
while (next_row(&id, &name)) {                   /* as many as there are */
    kvp_tupleset_begin(&a, &r.rows);
    kvp_tupleset_int32(&a, &r.rows, id);
    if (name) kvp_tupleset_text(&a, &r.rows, name); else kvp_tupleset_null(&a, &r.rows);
    kvp_tupleset_end(&a, &r.rows);
}
n = api_query_reply_size(&r, &a);                /* exact */
buf = malloc(n);
api_query_reply_encode(&r, &a, buf, n, &len);
kvp_buffer_free(&a);
```

Reading copies nothing: `api_query_reply_decode(&b, &r)` fills the struct
with offsets into the buffer that holds the message (`kvp_buffer_wrap`
over the bytes received), `kvp_st_ptr(&b, &r.note)` gives a field's bytes,
and `kvp_tupleset_open(&b, &r.kvp, key, &m, &def)` hands the tuples to the
reader functions above. The structs and their `_size`, `_encode`,
`_decode` and list functions are generated by the `kvpack` command; the
header documents the pieces they are made of (`kvp_put_*`, `kvp_read_*`),
which also serve to write such a struct by hand.

## Harbour

```xbase
#include "kvpack.ch"

hMsg := KvpNew( CMD_LOGIN )
KvpAddText( hMsg, K_USER, "bob" )
KvpAddTs( hMsg, K_WHEN, hb_UnixTime() )
cBytes := KvpEncode( hMsg )              // send it

hIn := KvpParse( cReply, @nError )       // NIL when not a valid message
IF hIn == NIL
   ? KvpErrorStr( nError )
ELSEIF KvpErrorText( hIn ) != NIL
   ? "server said:", KvpErrorText( hIn )
ELSE
   ? KvpGet( hIn, K_NAME )               // typed by the item: numeric, string or a nested handle
   FOR EACH hRow IN KvpGetAll( hIn, K_EVENTS )
      ? KvpGet( hRow, K_ID ), KvpGet( hRow, K_TITLE )
   NEXT
ENDIF
```

| function | does |
|---|---|
| `KvpNew( nCommand )` | a writer handle |
| `KvpAddInt32 / AddInt64 / AddTs / AddText / AddBin( hMsg, nKey, xValue )` | append an item |
| `KvpAddBool( hMsg, nKey, lValue )`, `KvpAddFloat64( hMsg, nKey, nValue )` | a logical, a double |
| `KvpAddDs( hMsg, nKey, dDate )` | a date, written as `DToS()` gives it; an empty date is the null ds |
| `KvpAddNull( hMsg, nKey )` | a null: the key is present and has no value |
| `KvpAddTupleDef( hMsg, nKey, aFields )` | the definition of the tuples that follow: `{ { cName, nType [, cMeta ] }, ... }`, `nType` a `KVP_INT32`..`KVP_DS` |
| `KvpAddTuple( hMsg, nKey, aValues )` | one tuple, right after its definition: one value per field, `NIL` for a null |
| `KvpAddMsg( hMsg, nKey, hSub )` | nest a message: a writer (encoded now) or a parsed one |
| `KvpEncode( hMsg )` | the bytes; the writer stays usable |
| `KvpParse( cBytes, [@nError] )` | a reader handle over a verified copy, or NIL |
| `KvpSize( cHeader, [@nError] )` | the total length of a message (header included) from its first 20 bytes, so a stream reader knows how much to read before `KvpParse`; NIL when they are no header |
| `KvpCommand / KvpCount( hMsg )` | command number, number of items |
| `KvpGet( hMsg, nKey, [nNth] )` | the value, typed by the item (numeric, string, logical, date or a nested handle; a tuple definition or a tuple as an array); NIL for a null and when absent |
| `KvpTupleDef( hMsg, nKey )` | the definition under the key as `{ { cName, nType, cMeta }, ... }`, or NIL |
| `KvpTuples( hMsg, nKey )` | its tuples: an array of arrays of values, `NIL` for a null field (empty when there are none; NIL when the key holds no definition) |
| `KvpIsNull( hMsg, nKey )` | `.T.` when the first item with the key is a null; `.F.` when it holds a value or is absent |
| `KvpGetAll( hMsg, nKey )` | every value under the key (a list) |
| `KvpItem( hMsg, nIndex )` | `{ nKey, nType, xValue }` of the nIndex-th item |
| `KvpType( hMsg, nKey )` | `KVP_INT32`.. `KVP_TUPLE`, 0 when absent |
| `KvpErrorText( hMsg )` | the text of an error reply, or NIL |
| `KvpErrorStr( nError )`, `KvpTypeStr( nType )` | names |

Handles are collected by the garbage collector: nothing to close, and a
nested handle from `KvpGet` owns its own copy, so parent and child live
independently. A wrong handle raises a catchable runtime error. Text
values are UTF-8 bytes both ways; converting to the program's code page
(`hb_UTF8ToStr` and back) is the caller's.

### Classes

`kvpclass.prg` (in `hbkvpack.hbp`, or added to a program's own sources)
puts two classes over those functions.

**`KvpTupleSet`** — a tuple definition and its tuples, one class for any
shape: the fields, and a two-dimensional array with the tuples.

```xbase
oSet := KvpTupleSet():New()
oSet:AddField( "id", KVP_INT32 )
oSet:AddField( "name", KVP_TEXT, "len=40" )     // name, type and the application's meta
oSet:AddRow( { 1, "Ana" } )
oSet:AddRow( { 2, NIL } )                       // NIL is a null
oSet:AddTo( hMsg, K_ROWS )                      // the definition and its tuples, under the key

oIn := KvpTupleSet():New()
IF oIn:Read( hReply, K_ROWS )
   FOR EACH aRow IN oIn:aRows
      ? aRow[ 1 ], aRow[ oIn:FieldPos( "name" ) ]
   NEXT
ENDIF
```

| member | does |
|---|---|
| `aFields` | `{ { cName, nType, cMeta }, ... }` (`KVP_FIELD_NAME`, `_TYPE`, `_META`) |
| `aRows` | the tuples: `aRows[ nTuple ][ nField ]`, `NIL` for a null |
| `AddField( cName, nType, [cMeta] )` | appends a field; `.F.` for an empty or repeated name, a type that is none of `KVP_INT32`..`KVP_DS`, or once there are tuples |
| `AddRow( aValues )` | appends a tuple; `.F.` when it does not have one value per field, each `NIL` or of its field's type |
| `Fits( aValues )` | whether the values are a tuple of the definition |
| `FieldPos( cName )` | the position of a field, 0 when there is none |
| `AddTo( hMsg, nKey )` | writes the definition and the tuples into a writer |
| `Read( hMsg, nKey )` | fills the object from a parsed message; `.F.` when the key holds no definition |

Both arrays may be assigned as they are (`oSet:aRows := aResult`); rows
that do not fit the definition then fail when the message is written.

**`KvpStruct`** — the base of the classes the `kvpack` command generates
from a `.kvp` (one per command, reply and record, a variable per field): a
class gives `Pack()` --> writer handle and `Unpack( hMsg, @nErr )` -->
`.T.`/`.F.`, and inherits `Encode()` --> bytes and `Decode( cBytes, @nErr )`.
See the main README.

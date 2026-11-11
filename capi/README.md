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
- `hbkvpack.c` / `kvpack.ch` / `hbkvpack.hbp` — the Harbour wrapper
  (see below) and `test_hbkvpack.prg` / `.hbp`, its test.

## Build and test

```bat
build.bat          :: MSVC, 32-bit, static CRT; builds and runs test_kvpack
build.bat x64      :: MSVC, 64-bit
make               :: gcc / MinGW / clang, -std=c89 -pedantic; builds and runs
hbmk2 hbkvpack.hbp        :: the Harbour wrapper as a static library
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
  is written as the null date), `_msg` (a message built by another
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
- `kvp_is_error(&m)` says whether a reply is an error reply.
- Every function returns `KVP_OK` (0) or a negative `kvp_error`;
  `kvp_error_str` names it. `KVP_E_MISSING` and `KVP_E_REPLY` are returned
  by code generated from a `.kvp` (see the main README).

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
| `KvpAddMsg( hMsg, nKey, hSub )` | nest a message: a writer (encoded now) or a parsed one |
| `KvpEncode( hMsg )` | the bytes; the writer stays usable |
| `KvpParse( cBytes, [@nError] )` | a reader handle over a verified copy, or NIL |
| `KvpCommand / KvpCount( hMsg )` | command number, number of items |
| `KvpGet( hMsg, nKey, [nNth] )` | the value, typed by the item (numeric, string, logical, date or a nested handle); NIL when absent |
| `KvpGetAll( hMsg, nKey )` | every value under the key (a list) |
| `KvpItem( hMsg, nIndex )` | `{ nKey, nType, xValue }` of the nIndex-th item |
| `KvpType( hMsg, nKey )` | `KVP_INT32`.. `KVP_DS`, 0 when absent |
| `KvpErrorText( hMsg )` | the text of an error reply, or NIL |
| `KvpErrorStr( nError )`, `KvpTypeStr( nType )` | names |

Handles are collected by the garbage collector: nothing to close, and a
nested handle from `KvpGet` owns its own copy, so parent and child live
independently. A wrong handle raises a catchable runtime error. Text
values are UTF-8 bytes both ways; converting to the program's code page
(`hb_UTF8ToStr` and back) is the caller's.

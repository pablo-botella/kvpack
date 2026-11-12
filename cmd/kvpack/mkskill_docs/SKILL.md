---
name: kvpack
description: "Tiny binary format that packs the data of a call (request or reply) between Go, C and Harbour programs: a command number plus typed key/value items with a crc32, 20-byte header, no transport, nothing beyond the standard library of each language, optional code generation from a .kvp declaration. Use when a Go server talks to native clients (C, Harbour, xBase++) over HTTP or a socket, as another alternative to JSON or gRPC. Covers the Go package (build, encode, decode, read from a stream), the wire format, the C and Harbour writer/reader in capi/, and the kvpack command that generates Go, C and Harbour code from a .kvp file. Triggers on kvpack, KVP messages, .kvp files, or \"binary request/response between Go and C/Harbour\" tasks."
---

# kvpack

## Overview

`kvpack` is a tiny binary format that packs the data of a call (request or
reply) between **Go**, **C** and **Harbour** programs: a `uint32` command
plus a flat list of typed key/value items (`uint16` key, `uint16` type,
`uint32` size, value), with a crc32 over the payload. 20-byte header, 8
bytes per item, little-endian. No transport, nothing beyond the standard
library of each language; code
generation from a `.kvp` declaration is optional.

Module: `github.com/pablo-botella/kvpack`. The package imports only the
standard library; the `kvpack` command, in the same module, also uses
`mkskill` for its embedded docs. The C writer/reader (ANSI C; `kvpack.c`
allocates nothing) and the Harbour wrapper live in `capi/`; all three
implementations are tested against the same canonical messages.

```sh
go get github.com/pablo-botella/kvpack
```

### When to use

- A Go server talking to native clients (C, Harbour, xBase++) that must stay
  dependency free, over any transport: the message is bytes (`Encode`/`WriteTo`
  out, `Decode`/`Read` in).
- Replies that carry records and lists: nest a message as a value (`Msg`)
  and repeat the key.
- A handful of typed fields, or query results, where JSON is too loose and
  a standard format such as Protocol Buffers has no binding for one of the
  languages involved (Harbour).

It is an alternative for that case, not a replacement for a standard: it is
not smaller than Protocol Buffers and has not been benchmarked. Where the
languages are well served by a standard format, prefer the standard.

### Types

| type | Go | value |
|---|---|---|
| `Int32` (1) | `AddInt32` / `Int32` | 4 bytes signed |
| `Int64` (2) | `AddInt64` / `Int64` | 8 bytes signed |
| `Time` (3) | `AddTime` / `Time` | 8 bytes: Unix seconds, UTC (`ts`) |
| `Text` (4) | `AddText` / `Text` | UTF-8, no terminator |
| `Bin` (5) | `AddBin` / `Bin` | raw bytes |
| `Msg` (6) | `AddMessage` / `Message` | a whole message nested; its command = record kind |
| `Bool` (7) | `AddBool` / `Bool` | 1 byte, 0 or 1 |
| `Float64` (8) | `AddFloat64` / `Float64` | 8 bytes IEEE 754 |
| `Date` (9) | `AddDate` / `Date` | 8 ASCII digits `yyyymmdd` (`ds`, Harbour `DToS`); 8 spaces = null date, and anything malformed reads as null (`SToD`) |
| `Null` (10) | `AddNull` / `IsNull` | nothing, size 0: the key is present and has no value; the typed getters say `false` on it |
| `TupDef` (11), `Tup` (12) | `AddTupleSet` / `TupleSet` | a definition (per field: type, name, opaque meta) and right after it, under the same key, its tuples (per field: type + size + value; a null field is the type `Null` with size 0) |

### Tuples (shape known only at run time)

Use them for the rows of a query or anything else whose fields the code
does not know in advance; use a record (`Msg`) when the shape is fixed.

```go
def := kvpack.NewTupleDef().Field("id", kvpack.Int32).FieldMeta("name", kvpack.Text, []byte("len=40"))
ts := kvpack.NewTupleSet(def)
err := ts.Add(int32(1), "Ana")      // one value per field, in order; nil = null
m.AddTupleSet(21, ts)

got, ok := m.TupleSet(21)           // got.Def.Fields[i].Name/.Type/.Meta, got.Rows [][]any
```

The order is strict and `Decode` enforces it: one definition per key, its
tuples right after it with nothing in between, every tuple with exactly the
fields of the definition. Fields are value types only (`Int32`..`Date`): no
tuples inside tuples. The meta is the application's, never interpreted.

Key `0` (`KeyError`) is reserved: a reply with a `Text` under it is an
error reply (`NewError`, `ErrorText`, `IsError`).

## Package API

### Build and encode

```go
m := kvpack.New(CmdLogin).AddText(KUser, "bob").AddTime(KWhen, time.Now())
b := m.Encode()                      // []byte, header included; m.Size() is len(b)
n, err := m.WriteTo(conn)            // one message onto a stream
e := kvpack.NewError(CmdLogin, "wrong password")   // a Text under KeyError
reply.AddMessage(KEvent, kvpack.New(RecEvent).AddInt32(KID, 7))   // a record; repeat the key for a list
```

`Add(key, Type, []byte)` is the raw form; it panics when an `Int32`/`Int64`/
`Time` value is not 4/8 bytes (programming error). `AddBin` keeps the slice
by reference.

### Decode and read

```go
m, err := kvpack.Decode(b)            // b is exactly one message; values alias b
m, err := kvpack.Read(r, max)         // one message from a stream, at most max bytes (<=0: no limit)
h, err := kvpack.DecodeHeader(b[:20]) // Command, Count, Size, CRC32 — how much follows

v, ok := m.Int32(key)    // first item with key, as int32; ok=false if absent OR another type
v, ok := m.Int64(key)
t, ok := m.Time(key)     // time.Time in UTC
s, ok := m.Text(key)
p, ok := m.Bin(key)      // aliases the message
sub, ok := m.Message(key)
text, isErr := m.ErrorText();  m.IsError()

it := m.Find(key)           // *Item or nil;  it.Int32(), it.Text(), it.Message()...
for _, it := range m.FindAll(key) { ... }   // a repeated key, in order
for _, it := range m.Items { it.Key, it.Type, it.Value }
```

Errors: `ErrMagic`, `ErrVersion`, `ErrTruncated`, `ErrFormat` (count,
sizes, unknown type, bad fixed size, trailing bytes, bad nested message),
`ErrCRC`, `ErrTooLarge` (`Read` over `max`). `Read` on an already-ended
stream returns `io.EOF`.

### Transport

The package has none. Over HTTP the message is the body:

```go
req, err := kvpack.Read(r.Body, 1<<20)                    // server: bounded read
w.Header().Set("Content-Type", "application/x-kvpack"); reply.WriteTo(w)
resp, err := http.Post(url, "application/x-kvpack", bytes.NewReader(req.Encode()))   // client
reply, err := kvpack.Read(resp.Body, 1<<20)
if text, bad := reply.ErrorText(); bad { ... }            // the server's error, status was 200
```

### C and Harbour (capi/)

```c
kvp_begin(&w, buf, cap, cmd); kvp_add_text(&w, k, "x"); kvp_add_ts(&w, k2, t); kvp_end(&w, &len);
kvp_parse(bytes, n, &m); kvp_get_int32(&m, k, &v); kvp_get_text(&m, k, dst, cap); kvp_get_msg(&m, k, &sub);
kvp_find(&m, k, &it); kvp_find_next(&m, &it);   /* lists */
```

```xbase
hMsg := KvpNew( nCmd ); KvpAddText( hMsg, nKey, cText ); cBytes := KvpEncode( hMsg )
hIn := KvpParse( cBytes, @nErr ); KvpGet( hIn, nKey ); KvpGetAll( hIn, nKey ); KvpErrorText( hIn )
```

Details and the full lists in `capi/README.md`.

## Declarations and the kvpack command

A `.kvp` file names commands, replies, records and their keys; `kvpack gen`
turns it into Go, C and Harbour code. Prefer it over hand-written key
constants when a project has more than a couple of commands.

```
command login = 1              # header: command name = number
    user      text   = 1       # field: name type = key [list | optional] [nullable]
    when      ts     = 3   optional
reply login                    # takes command login's number
    events    event  = 3   list
record event = 100             # a nested message; its number is the record kind
    id        int32  = 1
```

```sh
kvpack check api.kvp
kvpack gen -go api_kvp.go -pkg api -c api_kvp.h -hb api_kvp.prg [-prefix api] api.kvp
```

Outside the blocks: `service name` names the service the file declares (one
or more commands; the name is not on the wire, it replaces the file name as
the default of `-pkg` and `-prefix`), and `include "other.kvp"` reads
another file, path relative to the including one, into the same
declaration: one namespace, names and numbers unique across the files,
types resolved after everything is read. A file with `service` cannot be
included.

`names [lang [side]]` sections (lang `go`/`c`/`harbour`/`*`, side
`server`/`client`/`*`; omitted = any; most specific wins) name what is
generated: `command X = name`, `reply X = name`, `record X = name` (the type
of a block; `*` for all, with `*` in the name for each one's own),
`package = name` (Go), `prefix = name` (C, Harbour), and `service = name`
in a section with a side: the multiplexer (`server`) or the client
(`client`); default `Server`/`Client` in Go, the prefix in C and Harbour.
Lowercase with `_`,
converted per language; a type name is whole (no prefix added) and its
functions follow it. Types are named only in sections with no side.
`-pkg`/`-prefix` override.

- **Go:** `CmdLogin`, `RecEvent`; per block a struct (`Login`, `LoginReply`,
  `Event`) with `Pack() *kvpack.Message` and `Unpack(m) error`. Optional
  scalars are plain values (zero = absent), single records are pointers,
  lists are slices.
- **C:** `API_CMD_LOGIN`, keys `API_LOGIN_USER`; a fixed-size struct
  `api_login` over `kvp_arena.h` (compile `kvpack.c`, `dyn_buffer.c`,
  `kvp_arena.c`). Members are records: scalars `{ null, value }` assigned
  directly; text/bin/msg `{ null, size, off }` set with `kvp_set_text` /
  `kvp_set_bytes(&a, &s.f, ...)` and read with `kvp_st_ptr(&b, &s.f)`;
  records `{ null, has, value }`. Variable data lives in an arena, a
  `kvp_buffer` (`kvp_buffer_init(&a, malloc, free, 0, 0)` …
  `kvp_buffer_free(&a)`), by offset. `memset` the struct to zero first.
  Functions: `_size(&s, &a)` (exact message size), `_encode(&s, &a, buf,
  cap, &len)`, `_decode(&b, &s)` over a buffer holding the message
  (`kvp_buffer_wrap(&b, bytes, n)`, nothing copied), per list
  `_add_<f>(&a, &s, v)` and the walk `_<f>(&b, &s, &it, &v)` of a decoded
  struct (zero `it` first). A decoded struct encodes again, with the
  buffer it was decoded from, to the same bytes.
- **Harbour:** a class per block inheriting `KvpStruct` (link
  `capi/kvpclass.prg` with `kvpack.c` and `hbkvpack.c`): `o :=
  ApiLogin():New()`, a variable per field (`o:user := "bob"`),
  `o:Encode()` --> bytes, `o:Decode( cBytes, @nErr )` --> `.T.`/`.F.`, and
  `o:Pack()` / `o:Unpack( hMsg, @nErr )` over handles. Variables start at
  the empty value of their type: records, `msg` and tuplesets are created
  empty by `New()` (only an optional record or `msg` starts `NIL`). A
  record field holds an object of the record's class; a list of records is
  an array of them. Field names `new`, `init`, `encode`, `decode`, `error`,
  `classname` and the like are refused.
- **Server side** (all three): a
  multiplexer with no transport. It creates the message of the command,
  unpacks the request into it and calls the application's handler,
  `On<Command>`; replying is the handler's job, with the message and the
  context (the caller's, passed through untouched). Nobody is called for
  an unknown command or a missing required field.
  - Go: `type Handler interface { OnLogin(ctx context.Context, msg
    *LoginMsg) ... }`; `LoginMsg{Request Login; Reply LoginReply}` with
    `Pack()` and `PackError(text)`; `NewServer(h).Dispatch(ctx, m) error`.
  - C: `api_handler` (a struct of `void (*on_login)(void *app,
    api_login_msg *msg, void *ctx)`; `NULL` = not served);
    `api_login_msg { request; reply; b }` (`b`: the buffer the request
    lies in; the reply is encoded with its own `_encode` and the handler's
    arena); `api_dispatch(&h, app, &b, ctx)` --> `KVP_OK`, the error of
    `kvp_parse`, `KVP_E_NOT_FOUND` or `KVP_E_MISSING`.
  - Harbour: `oSrv := ApiServer():New( oHandler )`, `oSrv:Dispatch( hMsg,
    oCtx, @nErr )` --> `.T.`/`.F.` (`KVP_E_NOT_FOUND`, `KVP_E_MISSING`);
    handler methods `OnLogin( oMsg, oCtx )`; `oMsg` is an `ApiLoginMsg`
    (base `KvpMsg`): `request`, `reply` (`NIL` when the command has no
    reply), `Pack()`, `Encode()`, `PackError( cText )`.
  Each generator also writes a handler skeleton to copy, regenerated each
  run and not compiled: `<pkg>_handler-go.txt`, `<prefix>_handler-c.txt`,
  `<prefix>_handler-prg.txt`.
- **Client side** (all three), no transport in it either: a call per
  command over whatever carries it.
  - Go: `type Caller interface { Call(ctx, req *kvpack.Message)
    (*kvpack.Message, error) }`; `NewClient(c).Login(ctx, &req)
    (*LoginReply, error)`; a command with no reply returns `error`. An
    error reply is the error, with the server's text.
  - C: `api_client { int (*call)(void *transport, kvp_buffer *io); void
    *transport; }`: `call` sends the request `io` holds and leaves the
    reply in `io`, returning 0. `api_call_login(&c, &req, &a, &io,
    &reply)` (`io`: a growing `kvp_buffer` other than the arena `a`; the
    reply refers to it; no `reply` argument for a command with none) -->
    `KVP_OK`, `KVP_E_CALL` (the transport failed), `KVP_E_REPLY` (read
    the text with `kvp_error_text` over `io`), `KVP_E_MISSING`.
  - Harbour: `oCli := ApiClient():New( oTransport )`, `oTransport` any
    object with `Call( cRequest, @nErr )` --> reply bytes or `NIL`;
    `oCli:Login( oReq, @nErr )` --> the reply object or `NIL` (`.T.`/`.F.`
    for a command with no reply); `KVP_E_REPLY` with the text in
    `oCli:cError`, `KVP_E_CALL` when the transport gave no error of its
    own.
  Client and server can be joined in memory (a call straight into the
  multiplexer), which is how the tests run them.

| `.kvp` | Go | C | Harbour |
|---|---|---|---|
| `int32` / `int64` | `int32` / `int64` | `kvp_int32_st` / `kvp_int64_st` | numeric |
| `ts` | `time.Time` | `kvp_ts_st` (Unix seconds) | numeric (Unix seconds) |
| `text` | `string` | `kvp_text_st` | string |
| `bin` | `[]byte` | `kvp_bin_st` | string |
| `bool` | `bool` | `kvp_bool_st` | logical |
| `float64` | `float64` | `kvp_float64_st` | numeric |
| `ds` | `time.Time` (zero = null date) | `kvp_ds_st` `yyyymmdd` (0 = null date) | date (empty = null) |
| `msg` | `*kvpack.Message` | `kvp_msg_st` (encoded bytes; `kvp_st_msg` parses) | handle |
| record | `*Rec` (`[]Rec` in a list) | `<prefix>_<rec>_st`: `{ null, has, value }` | object of class `<Prefix><Rec>` |
| list | slice | `kvp_list_st` (`count`; `_add_<f>`, walk `_<f>`) | array |
| `tupleset` | `*kvpack.TupleSet` | `kvp_tupleset_st` (`nfields`, `ntuples`); built with `kvp_tupleset_field` / `_begin` / `_int32`… / `_null` / `_end(&a, &s.f)`, read with `kvp_tupleset_open(&b, &s.kvp, KEY, &m, &def)` then `kvp_tuple_next` / `kvp_tuple_field` | `KvpTupleSet` object: `aFields` `{ { cName, nType, cMeta }, ... }`, `aRows` (array of arrays, `NIL` = null), `AddField`, `AddRow`, `FieldPos` |

Modifiers: `optional`, `list` (the key repeats), `nullable` (the key may
hold a null instead of a value: Go `<Field>Null bool`, C the record's
`null`, Harbour `NIL`). They decide when a field is written:

| declared | written | when it does not arrive |
|---|---|---|
| (nothing) | always | decode fails |
| `optional` | when not empty (zero, no bytes) | reads empty |
| `nullable` | always, as value or as null | decode fails |
| `optional nullable` | when not null (a zero IS written) | reads null |

`nullable` goes with neither `list` nor `tupleset`. A `tupleset` declares
no fields — the definition travels in the message — and takes `optional`
only.

Empty values: zero, no bytes, false, null date, a message of command 0 and
no items, a record of empty fields, a tuple definition of no fields. A field
nobody set holds its empty value, so a struct that was only created (Go zero
value, with `nil` pointers; C `memset` to zero; Harbour `New()`) writes the
same bytes in the three languages: every non-optional field as its empty
value. An optional tupleset of no fields is not written and an absent one
reads as no fields. A record that always holds itself (through fields that
are neither optional nor lists) is refused.

Rules: a decode fails only when a required field is missing or of another
type — a null counts as another type unless the field is `nullable` (Go: an error naming it; C and Harbour: `KVP_E_MISSING`); a reply with
key 0 is an error reply (Go: the error is the server's text; C and Harbour:
`KVP_E_REPLY`). Unknown keys are ignored. Never edit the generated files:
change the `.kvp` and regenerate.

## Rules & gotchas

- **Keys and commands are the application's.** The format assigns no
  meaning; declare them once in a `.kvp` and generate the code (see above),
  or as constants on every side (`const` in Go, `#define` in C and in a
  `.ch` for Harbour) kept in step. Only key `0` is reserved (error text).
- **A typed getter never reinterprets.** `m.Text(k)` on an `Int32` item is
  `("", false)`, not a string of bytes. Use `Find`/`Items` to inspect the
  actual type.
- **First item wins.** `m.Int32(k)` and the C `kvp_get_*` take the first
  item with that key; a repeated key is a list, read with `FindAll` /
  `kvp_find_next` / `KvpGetAll`.
- **Decode validates everything, including nested messages.** A message
  that decodes is consistent: exact length, crc32, item count, every size,
  known types, fixed sizes, nested `Msg` values. Do not re-check; do check
  the *application* side (required keys, ranges).
- **`Decode` aliases, `Read` owns.** Values from `Decode(b)` point into `b`;
  copy a `Bin` or a nested message that must outlive the buffer. `Read`
  allocates its own bytes.
- **`Read` is bounded for a reason.** A hostile header can announce 4 GB;
  pass a `max` on anything that comes from a network.
- **No transport in the package.** Do not look for HTTP helpers: write the
  bytes with `Encode`/`WriteTo` and read them with `Read`/`Decode` over
  whatever channel the application has.
- **Error replies are messages.** Check `ErrorText`/`IsError` on every
  reply; over HTTP keep them on status 200 and leave other statuses to
  transport failures.
- **`Time` is seconds.** `AddTime` drops sub-second precision; the value is
  `time.Unix(sec, 0).UTC()` on the way back. Any location goes in; UTC comes
  out.
- **Text is UTF-8 by contract.** Neither side validates it. The C writer
  takes a NUL-terminated UTF-8 string; the C reader copies with a terminator
  and reports `KVP_E_FULL` when truncated. Harbour passes bytes through:
  convert with `hb_UTF8ToStr`/`hb_StrToUTF8` as needed.
- **`kvpack.c` allocates nothing** (the arena of the generated C structs
  does, through the application's `malloc`). `kvp_begin` needs a buffer sized with
  `kvp_room(nitems, value_bytes)`; a parsed `kvp_message` points into the
  caller's bytes, which must stay valid. The Harbour wrapper copies and the
  garbage collector frees.
- **Three implementations, one set of bytes.** The canonical message in the
  Go, C and Harbour tests is the contract; a change in one implementation
  that alters those bytes breaks the other two tests on purpose.
- **Format is version 1 and stable.** New types would be appended (10, 11...);
  a reader rejects an unknown type with `ErrFormat` rather than skipping it.

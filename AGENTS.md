# kvpack

## Overview

`kvpack` is a tiny binary format that packs the data of a call (request or
reply) between **Go**, **C** and **Harbour** programs: a `uint32` command
plus a flat list of typed key/value items (`uint16` key, `uint16` type,
`uint32` size, value), with a crc32 over the payload. 20-byte header, 8
bytes per item, little-endian. No transport, no dependencies; code
generation from a `.kvp` declaration is optional.

Module: `github.com/pablo-botella/kvpack` — standard library only. The
C writer/reader (ANSI C, no allocation) and the Harbour wrapper live in
`capi/`; all three implementations are tested against one canonical message.

```sh
go get github.com/pablo-botella/kvpack
```

### When to use

- A Go server talking to native clients (C, Harbour, xBase++) that must stay
  dependency free, over any transport: the message is bytes (`Encode`/`WriteTo`
  out, `Decode`/`Read` in).
- Replies that carry records and lists: nest a message as a value (`Msg`)
  and repeat the key.
- Any place JSON is too loose and gRPC/protobuf too heavy for a handful of
  typed fields.

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
    user      text   = 1       # field: name type = key [list | optional]
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

- **Go:** `CmdLogin`, `RecEvent`; per block a struct (`Login`, `LoginReply`,
  `Event`) with `Pack() *kvpack.Message` and `Unpack(m) error`. Optional
  scalars are plain values (zero = absent), single records are pointers,
  lists are slices.
- **C:** `API_CMD_LOGIN`, keys `API_LOGIN_USER`; `api_login` with
  `_encode`/`_decode`, `_write`/`_read`, per list `_add_<f>`, `_begin_<f>`
  (record element left open) and the walk `_<f>(&s, &it, &v)` (zero `it`
  first). Text is `kvp_utf8` and bin `kvp_bytes`, both pointing into the
  decoded bytes; an optional record has `has_<f>`.
- **Harbour:** `ApiLoginEncode( hS )`, `ApiLoginPack( hS )`,
  `ApiLoginDecode( cBytes, @nErr )`, `ApiLoginUnpack( hMsg, @nErr )` over
  hashes keyed by field name.

| `.kvp` | Go | C | Harbour |
|---|---|---|---|
| `int32` / `int64` | `int32` / `int64` | `int32_t` / `int64_t` | numeric |
| `ts` | `time.Time` | `int64_t` (Unix seconds) | numeric (Unix seconds) |
| `text` | `string` | `kvp_utf8` | string |
| `bin` | `[]byte` | `kvp_bytes` | string |
| `bool` | `bool` | `int` | logical |
| `float64` | `float64` | `double` | numeric |
| `ds` | `time.Time` (zero = null date) | `int32_t` `yyyymmdd` (0 = null) | date (empty = null) |
| `msg` | `*kvpack.Message` | `kvp_message` | handle |
| record | `*Rec` (`[]Rec` in a list) | struct by value (+ `has_<f>` if optional) | hash |

Rules: a decode fails only when a required field is missing or of another
type (Go: an error naming it; C and Harbour: `KVP_E_MISSING`); a reply with
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
- **The C side allocates nothing.** `kvp_begin` needs a buffer sized with
  `kvp_room(nitems, value_bytes)`; a parsed `kvp_message` points into the
  caller's bytes, which must stay valid. The Harbour wrapper copies and the
  garbage collector frees.
- **Three implementations, one set of bytes.** The canonical message in the
  Go, C and Harbour tests is the contract; a change in one implementation
  that alters those bytes breaks the other two tests on purpose.
- **Format is version 1 and stable.** New types would be appended (10, 11...);
  a reader rejects an unknown type with `ErrFormat` rather than skipping it.

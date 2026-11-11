# kvpack

A tiny binary format that packs the data of a call — a request or its
reply — between programs written in **Go**, **C** and **Harbour**: a
command number plus a flat list of typed key/value items, with a crc32 over
the payload. No transport, no dependencies: what the keys mean is the
application's contract, the format only carries them.

```sh
go get github.com/pablo-botella/kvpack
```

- **20-byte header, 8 bytes per item.** `command`, `nitems`, `size`, `crc32`;
  then `key`, `type`, `size`, value. Little-endian everywhere.
- **Typed values plus nesting** — `int32`, `int64`, `ts` (Unix seconds),
  `text` (UTF-8), `bin`, `bool`, `float64`, `ds` (a day, `yyyymmdd`) and
  `msg`: a whole message as a value, so a reply can carry records and lists
  of records.
- **Verified before use** — a decoder checks magic, version, item count, every
  size, every type and the crc32 (nested messages included) before handing
  anything back. A message that decodes is a message that is right.
- **Error replies are messages** — key 0 carries a text; the transport status
  stays out of it.
- **Three implementations, one set of bytes** — the Go package here, an ANSI C
  writer/reader and a Harbour wrapper in `capi/`, each tested against the same
  canonical message. All three write and read.
- **Any transport** — a message is bytes: an HTTP body, a socket, a file.
  `Read` takes exactly one from a stream, bounded.
- **Names when you want them** — declare commands, replies and records once
  in a `.kvp` file and the `kvpack` command generates Go structs, C structs
  and Harbour functions that pack, unpack and check required fields.

---

## Table of contents

- [Concepts](#concepts)
- [Quick start](#quick-start)
- [Go API](#go-api)
- [Carrying a message](#carrying-a-message)
- [Declarations and generated code](#declarations-and-generated-code)
- [Binary format](#binary-format)
- [C and Harbour](#c-and-harbour)
- [Design notes](#design-notes)
- [License](#license)

---

## Concepts

| Term | What it is |
|------|------------|
| **Message** | One unit on the wire: a 20-byte header (command, item count, data size, crc32) and the items. The same shape serves as request and as reply. |
| **Command** | A `uint32` the application assigns: *what* is asked (`login`, `clock-in`) or, in a nested message, *what kind of record* this is. The format gives it no meaning. |
| **Item** | A key (`uint16`), a type (`uint16`) and a value. Keys are the application's; a key may repeat, which is how a list travels. |
| **Type** | `int32` (4 bytes), `int64` (8), `ts` (8: Unix seconds, UTC), `text` (UTF-8, no terminator), `bin` (raw), `msg` (a whole message nested as the value), `bool` (1), `float64` (8, IEEE 754) or `ds` (8: a day as `yyyymmdd`, Harbour's `DToS`). |
| **Error reply** | A reply whose key 0 holds a text. `NewError` builds one, `ErrorText` reads it; a transport status (HTTP, say) stays at success because the message is the protocol. |
| **Canonical message** | A fixed message whose bytes are pinned in the Go, C and Harbour tests, so the three implementations are proven to agree byte for byte. |

The package **encodes and decodes** — it does not own a server, a socket or
a schema. Keys and commands are constants the application declares on each
side (a `const` block in Go, a `.h` in C, a `.ch` in Harbour), or once in a
`.kvp` declaration that the `kvpack` command turns into code for the three.
The declaration is not part of the format: the bytes are the same either way.

---

## Quick start

Declare the commands and keys once per side (they are plain numbers):

```go
const (
    CmdLogin uint32 = 1
    KUser    uint16 = 1
    KPass    uint16 = 2
    KWhen    uint16 = 3
    KName    uint16 = 1
    KExpires uint16 = 2
)
```

### Server (Go)

```go
func serve(req *kvpack.Message) *kvpack.Message {
    switch req.Command {
    case CmdLogin:
        user, _ := req.Text(KUser)
        pass, _ := req.Text(KPass)
        u, err := authenticate(user, pass)
        if err != nil {
            return kvpack.NewError(CmdLogin, "wrong user or password")
        }
        return kvpack.New(CmdLogin).AddText(KName, u.Name).AddTime(KExpires, u.Expires)
    }
    return kvpack.NewError(req.Command, "unknown command")
}
```

`req` comes from `kvpack.Read(body, 1<<20)` (bounded to 1 MB here); the
reply goes back with `WriteTo` — see [Carrying a message](#carrying-a-message).

### Client (Go)

```go
req := kvpack.New(CmdLogin).AddText(KUser, "bob").AddText(KPass, "secret").AddTime(KWhen, time.Now())
reply, err := send(req)                          // your transport: Encode out, Read back
if err != nil {
    return err                                   // transport or decoding
}
if text, bad := reply.ErrorText(); bad {
    return errors.New(text)                      // the server said no
}
name, _ := reply.Text(KName)
expires, _ := reply.Time(KExpires)
```

The same client in C or Harbour is in [`capi/README.md`](capi/README.md):
same bytes, same keys.

### Records and lists

A value can be a whole message (`Msg`), and a key can repeat:

```go
reply := kvpack.New(CmdEvents)
for _, e := range events {
    reply.AddMessage(KEvent, kvpack.New(RecEvent).AddInt32(KID, e.ID).AddText(KTitle, e.Title))
}
// reading
for _, it := range reply.FindAll(KEvent) {
    ev, _ := it.Message()
    id, _ := ev.Int32(KID)
}
```

---

## Go API

### Building

```go
func New(command uint32) *Message
func NewError(command uint32, text string) *Message   // a Text under KeyError (0)

func (m *Message) AddInt32(key uint16, v int32) *Message
func (m *Message) AddInt64(key uint16, v int64) *Message
func (m *Message) AddTime(key uint16, t time.Time) *Message   // stored as Unix seconds, UTC
func (m *Message) AddText(key uint16, s string) *Message
func (m *Message) AddBin(key uint16, b []byte) *Message       // kept by reference
func (m *Message) AddMessage(key uint16, sub *Message) *Message
func (m *Message) AddBool(key uint16, v bool) *Message
func (m *Message) AddFloat64(key uint16, v float64) *Message
func (m *Message) AddDate(key uint16, t time.Time) *Message   // the day of t in its own location; zero time = null date
func (m *Message) Add(key uint16, t Type, value []byte) *Message   // raw; panics on a value a decoder would refuse
```

The Add* methods return the message, so a reply reads as one expression.
Items keep the order they were added in.

### Encoding

```go
func (m *Message) Encode() []byte
func (m *Message) WriteTo(w io.Writer) (int64, error)
func (m *Message) Size() int        // encoded length, header included
func (m *Message) DataSize() int
```

### Decoding

```go
func Decode(b []byte) (*Message, error)            // b is exactly one message; values alias b
func Read(r io.Reader, max int) (*Message, error)  // one message from a stream, bounded; owns its bytes
func DecodeHeader(b []byte) (Header, error)        // the first 20 bytes: how much follows

type Header struct {
    Command uint32
    Count   uint32   // items
    Size    uint32   // bytes of data after the header
    CRC32   uint32
}
```

`Decode` checks magic, version, that `len(b)` is exactly header + size,
the crc32, then walks every item: count, each size inside the data,
known type, the fixed sizes, a `Bool` of 0 or 1, and recursively any
nested `Msg`. A `Date` is never refused: like Harbour's `SToD()`, what is
not a real day reads as the null date (the zero `time.Time`). It returns `ErrMagic`, `ErrVersion`, `ErrTruncated`,
`ErrFormat` or `ErrCRC`. `Read` adds `ErrTooLarge` when header + size
exceeds `max` (`max <= 0`: no limit); a stream that ends before the first
byte gives `io.EOF`, one that ends inside a message `ErrTruncated`.

### Reading

```go
func (m *Message) Int32(key uint16) (int32, bool)
func (m *Message) Int64(key uint16) (int64, bool)
func (m *Message) Time(key uint16) (time.Time, bool)      // UTC
func (m *Message) Text(key uint16) (string, bool)
func (m *Message) Bin(key uint16) ([]byte, bool)          // aliases the message
func (m *Message) Message(key uint16) (*Message, bool)    // a nested message
func (m *Message) Bool(key uint16) (bool, bool)
func (m *Message) Float64(key uint16) (float64, bool)
func (m *Message) Date(key uint16) (time.Time, bool)      // midnight UTC; zero time for the null date
func (m *Message) ErrorText() (string, bool)              // key 0 as text
func (m *Message) IsError() bool

func (m *Message) Find(key uint16) *Item       // first item with key, or nil
func (m *Message) FindAll(key uint16) []*Item  // every item with key, in order
```

The typed getters take the **first** item with that key and say `false`
when the key is absent **or holds another type**; a wrong type is never
silently reinterpreted. `Item` has the same typed methods (`it.Int32()`,
`it.Text()`, `it.Message()`...) for walking `m.Items` or `FindAll`.

```go
type Item struct {
    Key   uint16
    Type  Type
    Value []byte
}

type Type uint16
const (
    Int32 Type = 1
    Int64 Type = 2
    Time  Type = 3
    Text  Type = 4
    Bin   Type = 5
    Msg   Type = 6
    Bool    Type = 7
    Float64 Type = 8
    Date    Type = 9   // "ds" in the .kvp language
)
```

`Type.String()` gives `int32`, `int64`, `ts`, `text`, `bin`, `msg`,
`bool`, `float64`, `ds`.

---

## Carrying a message

kvpack has no transport: a message is a run of bytes and goes wherever bytes
go. `Encode`/`WriteTo` produce them; `Decode` takes bytes already in hand and
`Read(r, max)` takes exactly one message from a stream, leaving the next one
in it. The 20-byte header (`DecodeHeader`) is all a reader needs to know how
much to read.

As the body of an HTTP `POST`, for instance, it is a few lines on each side:

```go
// server
req, err := kvpack.Read(r.Body, 1<<20)
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}
w.Header().Set("Content-Type", "application/x-kvpack")
reply.WriteTo(w)

// client
resp, err := http.Post(url, "application/x-kvpack", bytes.NewReader(req.Encode()))
// check err and resp.StatusCode, then:
reply, err := kvpack.Read(resp.Body, 1<<20)
```

Keep an error reply (`NewError`) on status `200`: the message is the
protocol, the status reports only the transport. Always pass a `max` to
`Read` on anything that comes from a network.

---

## Declarations and generated code

Keys as bare numbers work, but they get repeated on every side. A `.kvp`
file declares them once, with names and types, and the `kvpack` command
generates the code that packs and unpacks each command, reply and record
in Go, C and Harbour:

```
# api.kvp
command login = 1
    user      text   = 1
    password  text   = 2
    when      ts     = 3   optional
reply login
    name      text   = 1
    expires   ts     = 2
    events    event  = 3   list

record event = 100
    id        int32  = 1
    title     text   = 2
```

One declaration per line, `#` starts a comment, indentation is only for
the eye. A header — `command name = number`, `reply name` (it takes the
number of its command) or `record name = number` — opens a block, and the
fields that follow belong to it: `name type = key [list | optional]`. The
types are `int32`, `int64`, `ts`, `text`, `bin`, `bool`, `float64`, `ds`, `msg` (any nested
message) or the name of a record. Commands and records share names and
numbers; keys are 1..65535 and unique in their block.

```sh
go install github.com/pablo-botella/kvpack/cmd/kvpack@latest

kvpack check api.kvp
kvpack gen -go api_kvp.go -pkg api -c api_kvp.h -hb api_kvp.prg api.kvp
```

`-c` writes the header and the `.c` next to it; `-hb` writes the `.prg` and
the `.ch` next to it; `-prefix` (C and Harbour names) and `-pkg` default
to the `.kvp` file name. In a Go project it fits a `//go:generate` line.

**What the generated code checks**, the same in the three languages: a
required field that is missing, or present with another type, fails the
decode; that is all. An optional field reads as zero when absent and is not
written when zero. A list is every item under its key (items of another
type skipped). Unknown keys are ignored, so a newer side can add fields. A
reply carrying key 0 decodes as an error reply. Single fields are written
first and lists after them, so the three write the same bytes.

How each declared type arrives in each language:

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
| record | `*Event` (`[]Event` in a list) | struct by value (+ `has_<f>` when optional) | hash (array of hashes in a list) |

**Go** — a constant per number and a struct per block:

```go
req := api.Login{User: "bob", Password: "secret"}
b := req.Pack().Encode()

var reply api.LoginReply
if err := reply.Unpack(m); err != nil { ... }   // missing field, or the server's error text
for _, e := range reply.Events { ... }
```

**C** — `#define`s, a `typedef struct` per block over `kvpack.h`, no
allocation: text and bin point into the decoded bytes (`kvp_utf8`,
`kvp_bytes`), a list is walked rather than stored.

```c
api_login req = {0};
req.user = kvp_utf8_of("bob");
req.password = kvp_utf8_of("secret");
api_login_encode(&req, buf, sizeof buf, &len);

api_login_reply r;
kvp_item it = {0};
api_event ev;
switch (api_login_reply_decode(bytes, n, &r)) {
case KVP_OK:
    while (api_login_reply_events(&r, &it, &ev) == KVP_OK) { ... }
    break;
case KVP_E_REPLY:                                /* kvp_error_text(&r.kvp, ...) */
    break;
}
```

Lists are written after the struct: `api_x_write(&w, &s)` between
`kvp_begin` and `kvp_end`, then `api_x_add_<field>(&w, ...)` per element
(`api_x_begin_<field>` leaves a record element open for its own lists).

**Harbour** — functions over a hash keyed by field name; lists are arrays,
records are hashes:

```xbase
cBytes := ApiLoginEncode( { "user" => "bob", "password" => "secret" } )
hR := ApiLoginReplyDecode( cReply, @nErr )     // NIL and nErr: KVP_E_MISSING, KVP_E_REPLY...
FOR EACH hEv IN hR[ "events" ]
   ? hEv[ "id" ], hEv[ "title" ]
NEXT
```

---

## Binary format

All integers little-endian. Version 1.

```
HEADER (20 bytes)
  0   magic     "KVP"
  3   version   uint8   1
  4   command   uint32  application command number
  8   nitems    uint32  number of items in DATA
  12  size      uint32  byte length of DATA
  16  crc32     uint32  crc32 (IEEE 802.3, the one of zlib/Go/mskblob) of DATA

DATA (size bytes): nitems items, back to back, no padding
  key       uint16  application key; 0 is reserved for an error text
  type      uint16  1 int32, 2 int64, 3 ts, 4 text, 5 bin, 6 msg,
                    7 bool, 8 float64, 9 ds
  size      uint32  byte length of value
  value     size bytes
```

| type | value |
|---|---|
| `int32` (1) | 4 bytes, signed |
| `int64` (2) | 8 bytes, signed |
| `ts` (3) | 8 bytes, signed: Unix seconds, UTC (`time_t`, `time.Unix`) |
| `text` (4) | UTF-8, no terminator |
| `bin` (5) | raw bytes |
| `msg` (6) | a whole message, header included; its `command` says what kind of record it is |
| `bool` (7) | 1 byte: 0 or 1 |
| `float64` (8) | 8 bytes, IEEE 754 |
| `ds` (9) | 8 ASCII digits `yyyymmdd`, a calendar day as Harbour's `DToS()` writes it; 8 spaces are the null date (`DToS()` of an empty date). As with `SToD()`, a reader takes anything that is not a real day (`00000000`, `20260230`) as the null date |

Rules a decoder enforces: the exact total length is header + `size`;
the crc32 matches; `nitems` items fit in `size` and consume it exactly;
every type is known and the fixed-size ones (`int32`, `int64`, `ts`,
`bool`, `float64`, `ds`) carry their size; a `bool` is 0 or 1; a `msg`
value decodes by the same rules. Anything else is rejected before
any value is handed out. A key may repeat; readers take the first unless
they ask for all.

The canonical message pinned in the three test suites — command 7 with an
`int32` −2, an `int64` 1234567890123, a `ts` 1700000000, the text `héllo`
and the bytes `00 ff 10` under keys 1..5 — is 89 bytes:

```
4b565001 07000000 05000000 45000000 a1233148     header: "KVP"1, cmd 7, 5 items, 69 bytes, crc
0100 0100 04000000 feffffff                       key 1 int32 -2
0200 0200 08000000 cb04fb711f010000               key 2 int64 1234567890123
0300 0300 08000000 00f1536500000000               key 3 ts 1700000000
0400 0400 06000000 68c3a96c6c6f                   key 4 text "héllo"
0500 0500 03000000 00ff10                         key 5 bin
```

A second one pins the later types — command 8 with a `bool` true, a `bool`
false, a `float64` 1.5, the `ds` 2026-11-11 and the null `ds` — in 86 bytes:

```
4b565001 08000000 05000000 42000000 9000fc76     header: "KVP"1, cmd 8, 5 items, 66 bytes, crc
0100 0700 01000000 01                             key 1 bool true
0200 0700 01000000 00                             key 2 bool false
0300 0800 08000000 000000000000f83f               key 3 float64 1.5
0400 0900 08000000 3230323631313131               key 4 ds "20261111"
0500 0900 08000000 2020202020202020               key 5 ds null (8 spaces)
```

---

## C and Harbour

`capi/` holds the same format for programs that are not Go, and unlike a
reader-only companion both sides **write and read**: a Harbour program or a
C client builds its requests and parses the replies with the same bytes
the Go package produces.

- `kvpack.h` / `kvpack.c` — ANSI C (C89 plus 64-bit integers), nothing
  beyond the C standard library, **no allocation**: the writer fills a
  buffer the caller provides, the reader points into the bytes the caller
  received. MSVC, MinGW, gcc, clang; 32 or 64 bit.
- `hbkvpack.c` / `kvpack.ch` — the Harbour wrapper over the C code:
  handles released by the garbage collector, `KvpGet` returning the value
  typed by the item (numeric, string or a nested handle).
- A test per language, all three pinning the **same canonical message**:
  `test_kvpack.c`, `test_hbkvpack.prg` and the Go test.

```c
kvp_begin(&w, buf, sizeof buf, CMD_LOGIN);
kvp_add_text(&w, K_USER, "bob");
kvp_add_ts(&w, K_WHEN, (int64_t)time(NULL));
kvp_end(&w, &len);                                   /* buf[0..len) is the message */

if (kvp_parse(reply, n, &m) == KVP_OK)              /* everything verified */
    kvp_get_text(&m, K_NAME, name, sizeof name);
```

```xbase
hMsg := KvpNew( CMD_LOGIN )
KvpAddText( hMsg, K_USER, "bob" )
cBytes := KvpEncode( hMsg )

hIn := KvpParse( cReply, @nError )
? KvpGet( hIn, K_NAME )
FOR EACH hRow IN KvpGetAll( hIn, K_EVENTS )
   ? KvpGet( hRow, K_ID )
NEXT
```

Build and the full function list are in [`capi/README.md`](capi/README.md).

---

## Design notes

**Why not JSON, gRPC or protobuf.** The consumers are a Go server and
native clients in C and Harbour (and xBase++) that must stay dependency
free: a self-contained 32-bit Windows executable importing only system
DLLs. gRPC in C drags in a C++ toolchain and tens of megabytes of
libraries; protobuf needs a schema compiler per language. A command number
plus typed key/value pairs is what those exchanges actually need, and it
is thirty lines to write and read in any language.

**Why typed items.** Opaque values would make the format two fields
smaller, but a type tag lets a reader refuse a wrong type instead of
reading garbage, lets a generic dump print a message legibly, and lets
Harbour's `KvpGet` return the right Harbour type without the caller
guessing. The scalar types are `int32`, `int64`, `ts`, `text`, `bin`,
`bool`, `float64` and `ds`. All signed — Harbour has no unsigned integers,
and anything unsigned fits in `int64`. `ts` is Unix seconds as `int64`, the
one time representation every side already has (`time_t`, `time.Unix`,
`hb_UnixTime`). Money travels as an `int64` of cents, not as a `float64`.

**Why `ds` is eight digits.** A day without a time (a schedule date, a
birthday) carried as a `ts` at midnight moves with the time zone. `ds`
carries the day itself, as the eight characters Harbour's `DToS()` writes
and `SToD()` reads: the same 8 bytes as a `ts`, only the representation
changes, it reads in a hex dump, and the null date is what `DToS()` gives
for an empty date, 8 spaces. Like `SToD()`, every side reads a malformed
`ds` (`00000000`, a 30th of February) as the null date instead of refusing
the message, and writes what is no real day as the null date. Go and C
read it as a `time.Time` and an `int32` `yyyymmdd`.

**Why `msg` as a type.** Lists of records (events, schedule rows) were the
one shape key/value pairs alone could not carry. Nesting a whole message
as a value gives records with their own command number as the record
kind, lists by repeating the key, and validation for free: a decoder
applies the same rules one level down. No depth limit is needed, since
every level costs 20 bytes of the parent's size.

**Why key 0 is the error.** A reply either carries what was asked or says
why not; making the error a message under a reserved key keeps the
transport status out of the protocol and keeps C and Harbour clients to
one code path. `NewError`/`ErrorText` and `kvp_error_text`/`KvpErrorText`
are the whole error API.

**Why `uint16` keys.** 65535 keys per command is plenty, and it keeps the
item header at 8 bytes with the type tag in. `command` stays `uint32`.

**Why no allocation in C.** The C side exists for small native clients and
for Harbour: a writer over a caller's buffer and a reader that points into
received bytes need no `malloc`, no `free` and no ownership rules. The
Harbour wrapper allocates — it has a garbage collector to lean on.

**Why one canonical message.** Three implementations agree because they
are proven to, byte for byte, on a message that exercises every type; the
Go test, the C test and the Harbour test all pin the same 89 bytes.

**What is deliberately out.** Transport (a message is bytes; `Read`/`WriteTo`
work over any stream), schemas, compression, encryption
(TLS is the transport's), and names for keys and commands on the wire. The
`.kvp` declaration and the `kvpack` generator give them names in the code
instead; the format did not change for it.

**Why the generated code checks so little.** It fails a decode only when a
required field is missing or of another type. Being strict about repeated
keys, unknown keys or command numbers would make the generated code bigger
and old clients brittle, and anything more is what schema systems such as
protobuf are for. Values (ranges, lengths) stay the application's.

---

## License

MIT.

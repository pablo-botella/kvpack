# kvpack

A tiny binary format that packs the data of a call — a request or its
reply — between programs written in **Go**, **C** and **Harbour**: a
command number plus a flat list of typed key/value items, with a crc32 over
the payload. No transport, and nothing beyond the standard library of each
language: what the keys mean is the
application's contract, the format only carries them.

```sh
go get github.com/pablo-botella/kvpack
```

- **20-byte header, 8 bytes per item.** `command`, `nitems`, `size`, `crc32`;
  then `key`, `type`, `size`, value. Little-endian everywhere.
- **Typed values plus nesting** — `int32`, `int64`, `ts` (Unix seconds),
  `text` (UTF-8), `bin`, `bool`, `float64`, `ds` (a day, `yyyymmdd`),
  `null` (present, no value) and `msg`: a whole message as a value, so a
  reply can carry records and lists of records.
- **Dynamic tuples** — for data whose shape is only known when the message
  is built (the rows of a query): a definition written on the fly (type,
  name and an opaque meta per field) followed by its tuples, each holding
  only values. Any field can be null.
- **Verified before use** — a decoder checks magic, version, item count, every
  size, every type and the crc32 (nested messages included) before handing
  anything back. A message that decodes is a well-formed message.
- **Error replies are messages** — key 0 carries a text; the transport status
  stays out of it.
- **Three implementations, one set of bytes** — the Go package here, an ANSI C
  writer/reader and a Harbour wrapper in `capi/`, each tested against the same
  canonical message. All three write and read.
- **Any transport** — a message is bytes: an HTTP body, a socket, a file.
  `Read` takes exactly one from a stream, bounded.
- **Names when you want them** — declare commands, replies and records once
  in a `.kvp` file and the `kvpack` command generates Go structs, C structs
  and Harbour classes that pack, unpack and check required fields.

---

## Table of contents

- [Why kvpack](#why-kvpack)
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

## Why kvpack

kvpack is offered as an alternative, not as a replacement: it does not try
to compete with Protocol Buffers or with any other official or widely
recognised standard.

It may suit a project that needs:

- **Simple for Harbour, and equal to Go and C.** One format, a reader and a
  writer in each language, and tests that pin the same bytes in all three.
- **Query results as they come.** A table travels with its own definition
  (names, types, nulls), so no schema is written per query.
- **A format it can maintain itself.** A 20-byte header and items of key,
  type, size and value. The whole specification fits on a page.

**It answers an immediate need.** The first plan was a fork of Protocol
Buffers with a Harbour layer, offered upstream as a proposal. That plan is
not dropped, but it may take months to bear fruit. kvpack solves today what
that work would solve later.

It is not smaller than Protocol Buffers, and nobody has measured it to be
faster. Where the languages involved are well served by a standard format,
or message size matters, the standard is the better choice. kvpack is for
the narrower case it was written for, and it is easy to keep.

---

## Concepts

| Term | What it is |
|------|------------|
| **Message** | One unit on the wire: a 20-byte header (command, item count, data size, crc32) and the items. The same shape serves as request and as reply. |
| **Command** | A `uint32` the application assigns: *what* is asked (`login`, `clock-in`) or, in a nested message, *what kind of record* this is. The format gives it no meaning. |
| **Item** | A key (`uint16`), a type (`uint16`) and a value. Keys are the application's; a key may repeat, which is how a list travels. |
| **Type** | `int32` (4 bytes), `int64` (8), `ts` (8: Unix seconds, UTC), `text` (UTF-8, no terminator), `bin` (raw), `msg` (a whole message nested as the value), `bool` (1), `float64` (8, IEEE 754), `ds` (8: a day as `yyyymmdd`, Harbour's `DToS`) `null` (0: the key is present and has no value), or the pair `tupledef` / `tuple`. |
| **Tuples** | A structure declared on the fly and then repeated: under one key a `tupledef` (per field its type, its name and a meta that is the application's) and right after it zero or more `tuple` items, each with one value per field, or null. |
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
func (m *Message) AddTime(key uint16, t time.Time) *Message   // stored as Unix seconds, UTC; the zero Time as 0
func (m *Message) AddText(key uint16, s string) *Message
func (m *Message) AddBin(key uint16, b []byte) *Message       // kept by reference
func (m *Message) AddMessage(key uint16, sub *Message) *Message
func (m *Message) AddBool(key uint16, v bool) *Message
func (m *Message) AddFloat64(key uint16, v float64) *Message
func (m *Message) AddDate(key uint16, t time.Time) *Message   // the day of t in its own location; zero time = null date
func (m *Message) AddNull(key uint16) *Message                // present, no value
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
known type, the fixed sizes, a `Bool` of 0 or 1, recursively any
nested `Msg`, and every tuple against its definition and its place. A `Date` is never refused: like Harbour's `SToD()`, what is
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
func (m *Message) IsNull(key uint16) bool                 // a Null under key; false when absent
func (m *Message) ErrorText() (string, bool)              // key 0 as text
func (m *Message) IsError() bool

func (m *Message) Find(key uint16) *Item       // first item with key, or nil
func (m *Message) FindAll(key uint16) []*Item  // every item with key, in order
```

The typed getters take the **first** item with that key and say `false`
when the key is absent **or holds another type**; a wrong type is never
silently reinterpreted. `Item` has the same typed methods (`it.Int32()`,
`it.Text()`, `it.Message()`...) for walking `m.Items` or `FindAll`.
A `Null` is such another type: every typed getter says `false` on it, and
`IsNull` (on the message or on an item) is what tells it from an absent key.

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
    Null    Type = 10  // no value: size 0
    TupDef  Type = 11  // the definition of the tuples that follow it
    Tup     Type = 12  // a tuple: the values of the fields of its definition
)
```

`Type.String()` gives `int32`, `int64`, `ts`, `text`, `bin`, `msg`,
`bool`, `float64`, `ds`, `null`, `tupledef`, `tuple`.

`it.Any()` gives the value as the Go type of the item's type (`int32`,
`int64`, `time.Time`, `string`, `[]byte`, `*Message`, `bool`, `float64`,
`*TupleDef`, `nil` for a `Null`), for walking what has no shape known in
advance.

### Tuples

For data whose shape is only known when the message is built — the rows of
a query — a structure is declared on the fly and then repeated:

```go
type TupleField struct {
    Name string
    Type Type     // Int32 .. Date
    Meta []byte   // the application's: carried as it is, never looked at
}
type TupleDef struct{ Fields []TupleField }
type TupleSet struct {
    Def  *TupleDef
    Rows [][]any  // one value per field; nil is a null
}

func NewTupleDef() *TupleDef
func (d *TupleDef) Field(name string, t Type) *TupleDef
func (d *TupleDef) FieldMeta(name string, t Type, meta []byte) *TupleDef

func NewTupleSet(def *TupleDef) *TupleSet
func (ts *TupleSet) Add(values ...any) error   // refused, and not added, when it does not fit the definition

func (m *Message) AddTupleSet(key uint16, ts *TupleSet) *Message   // the definition and one item per tuple
func (m *Message) TupleSet(key uint16) (*TupleSet, bool)
```

Neither side writes the fields in its code: the sender builds the
definition from what it has at hand, the receiver asks the definition what
came.

```go
def := kvpack.NewTupleDef()
for _, col := range columns {
    def.FieldMeta(col.Name, typeOf(col), col.Extra)
}
rows := kvpack.NewTupleSet(def)
for result.Next() {
    if err := rows.Add(result.Values()...); err != nil { ... }
}
m.AddTupleSet(21, rows)

// the other side
ts, _ := m.TupleSet(21)
for _, row := range ts.Rows {
    for i, v := range row {              // v is nil for a null
        fmt.Println(ts.Def.Fields[i].Name, "=", v)
    }
}
```

A row value is of the Go type of its field's type: `int32`, `int64`,
`time.Time` (`Time` and `Date`), `string`, `[]byte`, `*Message`, `bool`,
`float64`. `Add` also takes an `int` for an `Int32` (when it fits) or an
`Int64`. `Decode` has already checked every tuple against its definition
and the order of the items, so `TupleSet` on a decoded message cannot
fail for a malformed tuple. `TupleDef.EncodeRow`, `TupleDef.DecodeRow`,
`DecodeTupleDef` and `Item.TupleDef` work one item at a time.

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
fields that follow belong to it: `name type = key [list | optional] [nullable]`. The
types are `int32`, `int64`, `ts`, `text`, `bin`, `bool`, `float64`, `ds`, `msg` (any nested
message), `tupleset` (a tuple definition and its tuples) or the name of a
record. Commands and records share names and numbers; keys are 1..65535
and unique in their block.

```
reply query
    code      int32    = 1
    note      text     = 2   nullable            # a text, or a null
    limit     int32    = 3   optional nullable   # a number, or null and then not sent
    rows      tupleset = 4                       # shape known only at run time
```

`optional` and `nullable` are two different things: the first says the key
may be missing, the second that it may arrive as a `null` instead of a
value. Between them they decide when a field is written, and what one that
did not arrive means:

| declared | written | when it does not arrive |
|---|---|---|
| (nothing) | always | the decode fails |
| `optional` | when it is not empty (zero, no bytes) | it reads empty |
| `nullable` | always, as its value or as a null | the decode fails |
| `optional nullable` | when it is not null | it reads null |

`optional` leaves out the empty value and `optional nullable` leaves out
the null, so a field has two states at most. A `tupleset` declares nothing
about its fields: the definition travels in the message. It takes
`optional`, not `list` (it already is one) nor `nullable`.

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
type skipped). A null is "another type" unless the field is `nullable`:
then the decode takes it and says so apart from the value; an `optional
nullable` field is not written when null and reads null when absent. A
required `tupleset` needs its definition (no tuples is fine, and so is no
fields). Unknown keys are ignored, so a newer side can add fields. A reply
carrying key 0 decodes as an error reply. Single fields are written first
and lists and tuplesets after them, so the three write the same bytes.

**The empty value.** Every type has one: zero, no bytes, false, the null
date, a message of command 0 and no items, a record whose fields are all
empty, a tuple definition of no fields. It is what `optional` leaves out and
what a field nobody set holds, so a struct that was only created writes
the same bytes in the three languages: every field that is not optional, as
its empty value. A Go pointer left at `nil` (a `msg`, a record, a `tupleset`) is
that empty value; so are a C struct at zero and a new Harbour object. A
`time.Time` at its zero goes as 0. A record that would always hold itself
(through fields that are neither optional nor lists) has no empty value and
is refused.

How each declared type arrives in each language:

| `.kvp` | Go | C | Harbour |
|---|---|---|---|
| `int32` / `int64` | `int32` / `int64` | `kvp_int32_st` / `kvp_int64_st`: `{ null, value }` | numeric |
| `ts` | `time.Time` | `kvp_ts_st` (Unix seconds) | numeric (Unix seconds) |
| `text` | `string` | `kvp_text_st`: `{ null, size, off }` | string |
| `bin` | `[]byte` | `kvp_bin_st` | string |
| `bool` | `bool` | `kvp_bool_st` | logical |
| `float64` | `float64` | `kvp_float64_st` | numeric |
| `ds` | `time.Time` (zero = null date) | `kvp_ds_st`: `yyyymmdd` (0 = null date) | date (empty = null) |
| `msg` | `*kvpack.Message` | `kvp_msg_st`: the encoded bytes | handle |
| record | `*Event` (`[]Event` in a list) | `api_event_st`: `{ null, has, value }` | an `ApiEvent` object (an array of them in a list) |
| a `list` | a slice | `kvp_list_st`, filled and walked through functions | array |
| `tupleset` | `*kvpack.TupleSet` | `kvp_tupleset_st`, filled and walked through functions | a `KvpTupleSet` object: `aFields` and `aRows` |
| `nullable` | a `<Field>Null bool` beside the field | the `null` every record has | the value is `NIL` |

**Go** — a constant per number and a struct per block:

```go
req := api.Login{User: "bob", Password: "secret"}
b := req.Pack().Encode()

var reply api.LoginReply
if err := reply.Unpack(m); err != nil { ... }   // missing field, or the server's error text
for _, e := range reply.Events { ... }
```

**C** — `#define`s and a `typedef struct` per block, over `kvp_arena.h`.
The struct has a fixed size and is the caller's; its members are small
records, the same whatever the field declares: a scalar is `{ null, value }`
and is assigned directly, and what has no fixed size — a text, a list, the
tuples of a query — is copied into an **arena** and referred to by offset.
The arena is a `kvp_buffer` (`dyn_buffer.h`): one block that grows through
the `malloc` and `free` the application hands in, released whole with one
call. So a message whose size nobody knows in advance is built piece by
piece, its exact size asked for, and written into a buffer the caller
allocates; `kvpack.c` itself still allocates nothing.

```c
kvp_buffer a;
api_login req;
kvp_buffer_init(&a, malloc, free, 0, 0);
memset(&req, 0, sizeof req);                     /* everything empty, nothing null */
kvp_set_text(&a, &req.user, "bob");
kvp_set_text(&a, &req.password, "secret");
req.when.value = time(NULL);
n = api_login_size(&req, &a);                    /* the exact size */
buf = malloc(n);
api_login_encode(&req, &a, buf, n, &len);
kvp_buffer_free(&a);
```

Reading copies nothing: the bytes received are the buffer, and the decoded
struct refers to them.

```c
kvp_buffer b;
api_login_reply r;
kvp_item it = {0};
api_event ev;
kvp_buffer_wrap(&b, bytes, n);
switch (api_login_reply_decode(&b, &r)) {
case KVP_OK:
    use(kvp_st_ptr(&b, &r.name), r.name.size);
    while (api_login_reply_events(&b, &r, &it, &ev) == KVP_OK) { ... }
    break;
case KVP_E_REPLY:                                /* kvp_span_message(&b, &r.kvp, &m), kvp_error_text(&m, ...) */
    break;
}
```

A list takes one element per call, `api_x_add_<field>(&a, &s, v)`, in any
order with the rest; a record element is a struct of its own, filled (its
own lists too) before it is added. A tupleset is built straight on its
member, and read from the message the struct was decoded from:

```c
kvp_tupleset_field(&a, &r.rows, KVP_INT32, "id", NULL, 0);
kvp_tupleset_field(&a, &r.rows, KVP_TEXT, "name", NULL, 0);
while (next_row()) {                             /* as many as there are */
    kvp_tupleset_begin(&a, &r.rows);
    kvp_tupleset_int32(&a, &r.rows, id);
    kvp_tupleset_null(&a, &r.rows);
    kvp_tupleset_end(&a, &r.rows);
}

if (kvp_tupleset_open(&b, &r.kvp, API_QUERY_REPLY_ROWS, &m, &def) == KVP_OK)
    for (row = def.item; kvp_tuple_next(&m, &row) == KVP_OK; )
        if (kvp_tuple_field(&def, &row, 0, &f) == KVP_OK) ...   /* f.type is KVP_NULL for a null */
```

A field is null when its `null` is set (`r.note.null = 1`), and reads the
same way. An offset stays good when the arena grows; a pointer taken from
it (`kvp_st_ptr`) does not. A decoded struct can be encoded again with the
buffer it was decoded from: its lists and tuplesets are written as they
arrived.

**Harbour** — a class per block, with a variable per field and `Encode()`
and `Decode()` from the `KvpStruct` they inherit (`capi/kvpclass.prg`). A
list is an array; a record is an object of its own class, and so is each
element of a list of records; a nullable field that is null holds `NIL`. A
new object has its records, messages and tuplesets already there, empty
and ready to fill; only an optional record or message starts as `NIL`:

```xbase
oReq := ApiLogin():New()
oReq:user     := "bob"
oReq:password := "secret"
cBytes := oReq:Encode()

oR := ApiLoginReply():New()
IF oR:Decode( cReply, @nErr )                  // .F. and nErr: KVP_E_MISSING, KVP_E_REPLY...
   FOR EACH oEv IN oR:events                   // ApiEvent objects
      ? oEv:id, oEv:title, oEv:who:name
   NEXT
ENDIF
```

A tupleset is one class for all of them, `KvpTupleSet`: the fields and a
two-dimensional array with the tuples.

```xbase
oR:rows:AddField( "id", KVP_INT32 )              // the tupleset is already there
oR:rows:AddField( "name", KVP_TEXT, "len=40" ) // name, type and the application's meta
oR:rows:AddRow( { 1, "Ana" } )
oR:rows:AddRow( { 2, NIL } )                   // NIL is a null

? oR:rows:aRows[ 2 ][ oR:rows:FieldPos( "name" ) ]
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
                    7 bool, 8 float64, 9 ds, 10 null, 11 tupledef, 12 tuple
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
| `null` (10) | nothing: size 0. The key is present and has no value, which is not a zero, an empty text or an absent key |
| `tupledef` (11) | the definition of the tuples that follow it under its key: `nfields uint32`, then per field `type uint16`, `name size uint32`, the name (UTF-8), `meta size uint32`, the meta (raw bytes, the application's, never looked at) |
| `tuple` (12) | one tuple of the definition before it: per field, in order, `type uint16`, `size uint32` and the value. The type is the one the definition declares for the field, or `null` (10) with size 0 when the field is null |

Rules a decoder enforces: the exact total length is header + `size`;
the crc32 matches; `nitems` items fit in `size` and consume it exactly;
every type is known and the fixed-size ones (`int32`, `int64`, `ts`,
`bool`, `float64`, `ds`, and `null` with its size 0) carry their size; a
`bool` is 0 or 1; a `msg`
value decodes by the same rules. Anything else is rejected before
any value is handed out. A key may repeat; readers take the first unless
they ask for all.

Tuples add an order, and it is strict: a key holds at most one `tupledef`;
a `tuple` comes right after its `tupledef` or after another `tuple` of the
same key, with no other item in between; a field is of a value type
(`int32` to `ds`: no `null`, no tuples inside tuples), with a name that is
neither empty nor repeated; and every `tuple` carries exactly the fields of
its definition, each of the type declared for it and the size that type
demands (a `bool` 0 or 1, a `msg` that decodes), or a null. A value in a
tuple is an item without its key: null is a type there as everywhere else.
A `tupledef` with no tuples after it is an empty set.

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

A third one pins `null` — command 9 with an `int32` 7, a `null` and an
empty text, which are three different things — in 48 bytes:

```
4b565001 09000000 03000000 1c000000 a2ae13ae     header: "KVP"1, cmd 9, 3 items, 28 bytes, crc
0100 0100 04000000 07000000                       key 1 int32 7
0200 0a00 00000000                                key 2 null
0300 0400 00000000                                key 3 text ""
```

A fourth one pins tuples — command 10 with, under key 1, a definition
(`id int32`, `name text` with the meta `len=40`, `born ds`) and two tuples,
and under key 2 a definition with no tuples — in 176 bytes:

```
4b565001 0a000000 04000000 9c000000 f158c35d     header: "KVP"1, cmd 10, 4 items, 156 bytes, crc
0100 0b00 32000000                                key 1 tupledef, 50 bytes
    03000000                                          3 fields
    0100 02000000 6964     00000000                   int32 "id", no meta
    0400 04000000 6e616d65 06000000 6c656e3d3430      text "name", meta "len=40"
    0900 04000000 626f726e 00000000                   ds "born", no meta
0100 0c00 21000000                                key 1 tuple, 33 bytes
    0100 04000000 01000000                            int32 1
    0400 03000000 416e61                              text "Ana"
    0900 08000000 3139383030313135                    ds "19800115"
0100 0c00 1a000000                                key 1 tuple, 26 bytes
    0100 04000000 02000000                            int32 2
    0400 04000000 4c756973                            text "Luis"
    0a00 00000000                                     null
0200 0b00 0f000000                                key 2 tupledef, 15 bytes, no tuples
    01000000                                          1 field
    0200 01000000 6e 00000000                         int64 "n", no meta
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
- `dyn_buffer.h` / `kvp_arena.h` (and their `.c`) — optional, for when
  the size of a message is not known in advance: a buffer that grows
  through the `malloc` and `free` the application hands in, and over it
  messages as fixed structs whose variable data lives in that one block.
  The C code generated from a `.kvp` is built on them.
- `kvpclass.prg` — Harbour classes over the wrapper: `KvpTupleSet` (the
  fields and a two-dimensional array of tuples) and `KvpStruct`, the base
  of the classes generated from a `.kvp`.
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
native clients in C and Harbour (and xBase++), one of them a
self-contained 32-bit Windows executable importing only system DLLs. JSON
has no binary values and few types of its own. gRPC brings a transport and
a runtime those clients have no use for. Protocol Buffers alone would have
served the C side, where small implementations exist, but no Harbour
binding was at hand, and the format had to stay simple on the Harbour
side: something a Harbour program reads and writes with a few calls, and
its maintainer can follow end to end. A command number plus typed
key/value pairs is that. [Why kvpack](#why-kvpack) says where that leaves
it.

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

**Why `null` is a type.** A value that is unknown is not a zero, an empty
text or a missing key: a row of a query result has every column, and some
of them hold nothing. `null` is an item with its key and no value (size 0),
so it costs the 8 bytes of the item header and needs no flag elsewhere. A
typed reader never takes it for a value: asking a null for an `int32` says
"another type", as with any other mismatch.

**Why tuples, and why with a definition.** A record has a shape fixed in
advance; the rows of a query have one that is only known when they come
back. A tuple is that: a structure declared on the fly — a `tupledef` with
the type, the name and a meta of each field — and then repeated, each
`tuple` holding only values. The definition travels once — names and metas
are not repeated — and each value of a row is an item without its key:
type, size, value, 6 bytes of overhead. The type is said again in every
row so that null stays what it is everywhere else, a type, and not a
reserved size or a bitmap bolted on for tuples. Every row is the same by
construction: a decoder checks each one against its definition, type by
type, and refuses the message otherwise. That is also why the order
is strict (one definition per key, its tuples right after it): a reader
walks the message once and always knows which definition a tuple belongs
to. The meta is raw bytes the format never looks at — whatever the
application wants to say about a column beyond its type. A tuple holds no
tuples; a field that needs structure is a `msg`, or a `bin` the application
reads its own way.

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
Harbour wrapper allocates — it has a garbage collector to lean on. The
structs generated from a `.kvp` are the other exception, on purpose: a
message whose size nobody knows in advance grows in an arena, through the
`malloc` and `free` the application hands in. `kvpack.c` itself still
allocates nothing.

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

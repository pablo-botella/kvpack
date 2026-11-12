---
mkskill:
  pos: 70
  in: readme
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

A declaration can say which **service** it is and be spread over several
files:

```
# sqlbk.kvp
service sqlbk
include "sqlbk_types.kvp"

command query = 1
    sql       text     = 1
reply query
    results   result   = 1   list      # result is declared in sqlbk_types.kvp
```

`service` groups the commands of the file, one or more, under a name. The
name does not travel in the messages: it is what the generated names start
from, instead of the file name. `include` reads another file, its path
relative to the one that includes it, into the same declaration: one
namespace, where no name or number repeats and a type may be declared in
any of the files. A file that declares a service is a declaration on its
own and cannot be included.

The names of what is generated can be part of the declaration too, per
language, in `names` sections:

```
names                              # every language
    record   result = sqlbk_record
names go                           # Go alone
    package         = sqlbk
names c                            # C alone
    command  *      = call_sqlbk_*   # call_sqlbk_query, ...
```

The header is `names [lang [side]]` (`go`, `c`, `harbour` or `*`; `server`,
`client` or `*`), and the most specific section wins. `command`, `reply`
and `record` name the type of a block, `*` standing for all of them;
`package` and `prefix` are what `-pkg` and `-prefix` give on the command
line, which still have the last word. Names are written in lowercase with
`_` and each language dresses them its own way: `sqlbk_query` is the struct
`SqlbkQuery` in Go, `sqlbk_query` in C and the class `SqlbkQuery` in Harbour.
A language has one set of types, shared by a client and a server, so a type
is named in a section with no side. The side is for `service`, which names
the two things that do differ, the multiplexer of the server and the client
(both described below):

```
names go server
    service = sqlbk_service        # the multiplexer: SqlbkService, and its handler SqlbkServiceHandler
names c client
    service = sqlbk                # the client: sqlbk_client, and the calls sqlbk_call_<command>
```

Without it they are `Server` and `Client` in Go, where the package tells them
apart, and take the prefix in C and Harbour.

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

**The server side.** A declaration with commands also gives, in the three
languages, a **multiplexer**: the piece that takes a message that arrived,
finds its command and hands the call to the application. It has no
transport in it: whoever received the message calls it. Three things take
part:

- the **handler**, which is the application's: one function per command,
  named `On` and the command. It is given to the multiplexer;
- the **message**, which the multiplexer creates for each call: the request,
  already unpacked, and the reply, new and empty, to fill (a command that
  declares no reply has none);
- the **context**: whatever the caller gives for that call. The multiplexer
  passes it on and never looks at it.

The multiplexer creates the message, unpacks the request into it and calls
the handler; that is all. What is replied, and how it reaches the caller, is
the handler's, with the message and the context. It calls nobody when the
command is none of the declaration or the request misses a required field.

| | handler | message | multiplexer |
|---|---|---|---|
| Go | interface `Handler`: `OnLogin(ctx, msg)` | `*LoginMsg`: `Request`, `Reply`, `Pack()`, `PackError(text)` | `NewServer(h).Dispatch(ctx, m)` --> `error` |
| C | struct `api_handler` of `on_login(app, msg, ctx)` | `api_login_msg`: `request`, `reply`, `b` (the buffer the request lies in) | `api_dispatch(&h, app, &b, ctx)` --> `KVP_OK` or an error |
| Harbour | object with `OnLogin( oMsg, oCtx )` | `ApiLoginMsg`: `request`, `reply`, `Pack()`, `Encode()`, `PackError( cText )` | `ApiServer():New( oHandler ):Dispatch( hMsg, oCtx, @nErr )` --> `.T.`/`.F.` |

```go
type MyHandler struct{}

func (h *MyHandler) OnLogin(ctx context.Context, msg *api.LoginMsg) {
    msg.Reply.Name = strings.ToUpper(msg.Request.User)   // the reply is there: just fill it
    send(ctx, msg.Pack())                                // the context is the caller's; send is the application's
}

srv := api.NewServer(&MyHandler{})
err := srv.Dispatch(ctx, m)                              // m: a decoded message
```

```xbase
METHOD OnLogin( oMsg, oCtx ) CLASS MyHandler
   oMsg:reply:name := Upper( oMsg:request:user )
   oCtx:Send( oMsg:Encode() )
   RETURN NIL

oSrv := ApiServer():New( MyHandler():New() )
lOk  := oSrv:Dispatch( hMsg, oCtx, @nErr )               // hMsg: a message from KvpParse
```

```c
static void on_login(void *app, api_login_msg *msg, void *ctx)
{
    kvp_set_text(arena, &msg->reply.name, "ann");        /* an arena of the handler's own */
    api_login_reply_encode(&msg->reply, arena, buf, cap, &len);
}

api_handler h = { on_login, on_ping };
rc = api_dispatch(&h, app, &b, ctx);                     /* b: a buffer that holds the message */
```

In Go the context is a `context.Context`, in C a `void *`, in Harbour any
value. In Harbour the multiplexer keeps, per command, the class function of
its message (`oSrv:login`), so an application may put a class of its own
there; in C a function left at `NULL` is a command nobody serves.

Next to the code each generator writes a **skeleton of the handler**, with
every function and, in comments, the fields of each request and reply:
`<package>_handler-go.txt`, `<prefix>_handler-c.txt` and
`<prefix>_handler-prg.txt`. It is valid code to copy under a name of your own
and fill; it is written again on every run and is no part of the build.

**The client side** is the mirror of it, and has no transport in it either:
a call per command, which packs the request, hands it to whatever carries
the call and unpacks the reply. What carries it is the application's, or a
transport's: something that sends a message and returns the reply to it.

| | what carries a call | a call |
|---|---|---|
| Go | interface `Caller`: `Call(ctx, req) (reply, error)` | `NewClient(c).Login(ctx, &req)` --> `(*LoginReply, error)` |
| C | struct `api_client`: a function `call(transport, io)` and `transport` | `api_call_login(&c, &req, &a, &io, &reply)` --> `KVP_OK` or an error |
| Harbour | any object with `Call( cRequest, @nErr )` --> the reply's bytes, or `NIL` | `ApiClient():New( oTransport ):Login( oReq, @nErr )` --> the reply object, or `NIL` |

```go
c := api.NewClient(caller)
reply, err := c.Login(ctx, &api.Login{User: "bob", Password: "secret"})
```

```xbase
oCli := ApiClient():New( oTransport )
oReply := oCli:Login( oReq, @nErr )
IF oReply == NIL .AND. nErr == KVP_E_REPLY
   ? oCli:cError                                         // the server's own text
ENDIF
```

```c
api_client c = { my_call, my_transport };
rc = api_call_login(&c, &req, &a, &io, &reply);          /* io: a growing kvp_buffer; the reply lies in it */
```

An error reply of the server is the error of the call: its text in Go, and
`KVP_E_REPLY` in C and Harbour (the text is then in `io`, for
`kvp_error_text`, and in `oCli:cError`). When what carries the call could not,
Go returns its error as it is, and C and Harbour say `KVP_E_CALL`. A command
that declares no reply returns only whether it went well. Since neither
side knows of a transport, the two can be joined in memory, a call going
straight into a multiplexer: that is how the tests run them.

---

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

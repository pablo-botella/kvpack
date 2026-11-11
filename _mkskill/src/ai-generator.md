---
mkskill:
  pos: 220
  in: ai*
---

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

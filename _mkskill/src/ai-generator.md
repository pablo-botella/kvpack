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

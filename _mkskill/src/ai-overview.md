---
mkskill:
  pos: 200
  in: ai*
---

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

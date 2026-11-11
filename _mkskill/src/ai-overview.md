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

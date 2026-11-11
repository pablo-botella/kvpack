---
mkskill:
  pos: 10
  in: readme
---

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


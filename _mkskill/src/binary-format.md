---
mkskill:
  pos: 80
  in: readme
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


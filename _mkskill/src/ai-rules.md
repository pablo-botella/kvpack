---
mkskill:
  pos: 240
  in: ai*
---

## Rules & gotchas

- **Keys and commands are the application's.** The format assigns no
  meaning; declare them once in a `.kvp` and generate the code (see above),
  or as constants on every side (`const` in Go, `#define` in C and in a
  `.ch` for Harbour) kept in step. Only key `0` is reserved (error text).
- **A typed getter never reinterprets.** `m.Text(k)` on an `Int32` item is
  `("", false)`, not a string of bytes. Use `Find`/`Items` to inspect the
  actual type.
- **First item wins.** `m.Int32(k)` and the C `kvp_get_*` take the first
  item with that key; a repeated key is a list, read with `FindAll` /
  `kvp_find_next` / `KvpGetAll`.
- **Decode validates everything, including nested messages.** A message
  that decodes is consistent: exact length, crc32, item count, every size,
  known types, fixed sizes, nested `Msg` values. Do not re-check; do check
  the *application* side (required keys, ranges).
- **`Decode` aliases, `Read` owns.** Values from `Decode(b)` point into `b`;
  copy a `Bin` or a nested message that must outlive the buffer. `Read`
  allocates its own bytes.
- **`Read` is bounded for a reason.** A hostile header can announce 4 GB;
  pass a `max` on anything that comes from a network.
- **No transport in the package.** Do not look for HTTP helpers: write the
  bytes with `Encode`/`WriteTo` and read them with `Read`/`Decode` over
  whatever channel the application has.
- **Error replies are messages.** Check `ErrorText`/`IsError` on every
  reply; over HTTP keep them on status 200 and leave other statuses to
  transport failures.
- **`Time` is seconds.** `AddTime` drops sub-second precision; the value is
  `time.Unix(sec, 0).UTC()` on the way back. Any location goes in; UTC comes
  out.
- **Text is UTF-8 by contract.** Neither side validates it. The C writer
  takes a NUL-terminated UTF-8 string; the C reader copies with a terminator
  and reports `KVP_E_FULL` when truncated. Harbour passes bytes through:
  convert with `hb_UTF8ToStr`/`hb_StrToUTF8` as needed.
- **The C side allocates nothing.** `kvp_begin` needs a buffer sized with
  `kvp_room(nitems, value_bytes)`; a parsed `kvp_message` points into the
  caller's bytes, which must stay valid. The Harbour wrapper copies and the
  garbage collector frees.
- **Three implementations, one set of bytes.** The canonical message in the
  Go, C and Harbour tests is the contract; a change in one implementation
  that alters those bytes breaks the other two tests on purpose.
- **Format is version 1 and stable.** New types would be appended (10, 11...);
  a reader rejects an unknown type with `ErrFormat` rather than skipping it.

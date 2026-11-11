---
mkskill:
  pos: 90
  in: readme
---

## Design notes

**Why not JSON, gRPC or protobuf.** The consumers are a Go server and
native clients in C and Harbour (and xBase++) that must stay dependency
free: a self-contained 32-bit Windows executable importing only system
DLLs. gRPC in C drags in a C++ toolchain and tens of megabytes of
libraries; protobuf needs a schema compiler per language. A command number
plus typed key/value pairs is what those exchanges actually need, and it
is thirty lines to write and read in any language.

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
Harbour wrapper allocates — it has a garbage collector to lean on.

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


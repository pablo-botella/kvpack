---
mkskill:
  pos: 90
  in: readme
---

## Design notes

**Why not JSON, gRPC or protobuf.** The consumers are a Go server and
native clients in C and Harbour (and xBase++), one of them a
self-contained 32-bit Windows executable importing only system DLLs. JSON
has no binary values and few types of its own. gRPC brings a transport and
a runtime those clients have no use for. Protocol Buffers alone would have
served the C side, where small implementations exist, but no Harbour
binding was at hand, and the format had to stay simple on the Harbour
side: something a Harbour program reads and writes with a few calls, and
its maintainer can follow end to end. A command number plus typed
key/value pairs is that. [Why kvpack](#why-kvpack) says where that leaves
it.

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

**Why `null` is a type.** A value that is unknown is not a zero, an empty
text or a missing key: a row of a query result has every column, and some
of them hold nothing. `null` is an item with its key and no value (size 0),
so it costs the 8 bytes of the item header and needs no flag elsewhere. A
typed reader never takes it for a value: asking a null for an `int32` says
"another type", as with any other mismatch.

**Why tuples, and why with a definition.** A record has a shape fixed in
advance; the rows of a query have one that is only known when they come
back. A tuple is that: a structure declared on the fly — a `tupledef` with
the type, the name and a meta of each field — and then repeated, each
`tuple` holding only values. The definition travels once — names and metas
are not repeated — and each value of a row is an item without its key:
type, size, value, 6 bytes of overhead. The type is said again in every
row so that null stays what it is everywhere else, a type, and not a
reserved size or a bitmap bolted on for tuples. Every row is the same by
construction: a decoder checks each one against its definition, type by
type, and refuses the message otherwise. That is also why the order
is strict (one definition per key, its tuples right after it): a reader
walks the message once and always knows which definition a tuple belongs
to. The meta is raw bytes the format never looks at — whatever the
application wants to say about a column beyond its type. A tuple holds no
tuples; a field that needs structure is a `msg`, or a `bin` the application
reads its own way.

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
Harbour wrapper allocates — it has a garbage collector to lean on. The
structs generated from a `.kvp` are the other exception, on purpose: a
message whose size nobody knows in advance grows in an arena, through the
`malloc` and `free` the application hands in. `kvpack.c` itself still
allocates nothing.

**Why one canonical message.** Three implementations agree because they
are proven to, byte for byte, on a message that exercises every type; the
Go test, the C test and the Harbour test all pin the same 89 bytes.

**Why the generated server and client carry no transport.** A multiplexer
that takes a message and calls a function, and a call that packs a request
and unpacks a reply, are the same whatever brings the bytes: HTTP, a pipe,
a test that joins the two in memory. Keeping the transport out lets every
language have both sides, and lets each be tested without a network. The
multiplexer stops at calling the handler, and does not send the reply
itself, for the same reason: how a reply travels, and whether it is one
message, several or none, is for the handler and the context it was given.

**Why a service has a name and no number.** The service is a way of
grouping the commands of a declaration and of naming what is generated
from it; it is not part of a message. Which call goes where when a server
offers several is decided by whoever receives it, with what the transport
knows (a path, a header, who is asking), and that is not the format's
business. A number in the message would have made it so.

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


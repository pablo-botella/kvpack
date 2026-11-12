---
mkskill:
  pos: 85
  in: readme
---

## C and Harbour

`capi/` holds the same format for programs that are not Go, and unlike a
reader-only companion both sides **write and read**: a Harbour program or a
C client builds its requests and parses the replies with the same bytes
the Go package produces.

- `kvpack.h` / `kvpack.c` — ANSI C (C89 plus 64-bit integers), nothing
  beyond the C standard library, **no allocation**: the writer fills a
  buffer the caller provides, the reader points into the bytes the caller
  received. MSVC, MinGW, gcc, clang; 32 or 64 bit.
- `dyn_buffer.h` / `kvp_arena.h` (and their `.c`) — optional, for when
  the size of a message is not known in advance: a buffer that grows
  through the `malloc` and `free` the application hands in, and over it
  messages as fixed structs whose variable data lives in that one block.
  The C code generated from a `.kvp` is built on them.
- `kvpclass.prg` — Harbour classes over the wrapper: `KvpTupleSet` (the
  fields and a two-dimensional array of tuples) and `KvpStruct`, the base
  of the classes generated from a `.kvp`.
- `hbkvpack.c` / `kvpack.ch` — the Harbour wrapper over the C code:
  handles released by the garbage collector, `KvpGet` returning the value
  typed by the item (numeric, string or a nested handle).
- A test per language, all three pinning the **same canonical message**:
  `test_kvpack.c`, `test_hbkvpack.prg` and the Go test.

```c
kvp_begin(&w, buf, sizeof buf, CMD_LOGIN);
kvp_add_text(&w, K_USER, "bob");
kvp_add_ts(&w, K_WHEN, (int64_t)time(NULL));
kvp_end(&w, &len);                                   /* buf[0..len) is the message */

if (kvp_parse(reply, n, &m) == KVP_OK)              /* everything verified */
    kvp_get_text(&m, K_NAME, name, sizeof name);
```

```xbase
hMsg := KvpNew( CMD_LOGIN )
KvpAddText( hMsg, K_USER, "bob" )
cBytes := KvpEncode( hMsg )

hIn := KvpParse( cReply, @nError )
? KvpGet( hIn, K_NAME )
FOR EACH hRow IN KvpGetAll( hIn, K_EVENTS )
   ? KvpGet( hRow, K_ID )
NEXT
```

Build and the full function list are in [`capi/README.md`](capi/README.md).

---


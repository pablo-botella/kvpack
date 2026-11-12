---
mkskill:
  pos: 25
  in: readme
---

## Why kvpack

kvpack is offered as an alternative, not as a replacement: it does not try
to compete with Protocol Buffers or with any other official or widely
recognised standard.

It may suit a project that needs:

- **Simple for Harbour, and equal to Go and C.** One format, a reader and a
  writer in each language, and tests that pin the same bytes in all three.
- **Query results as they come.** A table travels with its own definition
  (names, types, nulls), so no schema is written per query.
- **A format it can maintain itself.** A 20-byte header and items of key,
  type, size and value. The whole specification fits on a page.

**It answers an immediate need.** The first plan was a fork of Protocol
Buffers with a Harbour layer, offered upstream as a proposal. That plan is
not dropped, but it may take months to bear fruit. kvpack solves today what
that work would solve later.

It is not smaller than Protocol Buffers, and nobody has measured it to be
faster. Where the languages involved are well served by a standard format,
or message size matters, the standard is the better choice. kvpack is for
the narrower case it was written for, and it is easy to keep.

---


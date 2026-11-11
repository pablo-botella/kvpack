---
mkskill:
  pos: 30
  in: readme
---

## Concepts

| Term | What it is |
|------|------------|
| **Message** | One unit on the wire: a 20-byte header (command, item count, data size, crc32) and the items. The same shape serves as request and as reply. |
| **Command** | A `uint32` the application assigns: *what* is asked (`login`, `clock-in`) or, in a nested message, *what kind of record* this is. The format gives it no meaning. |
| **Item** | A key (`uint16`), a type (`uint16`) and a value. Keys are the application's; a key may repeat, which is how a list travels. |
| **Type** | `int32` (4 bytes), `int64` (8), `ts` (8: Unix seconds, UTC), `text` (UTF-8, no terminator), `bin` (raw), `msg` (a whole message nested as the value), `bool` (1), `float64` (8, IEEE 754) or `ds` (8: a day as `yyyymmdd`, Harbour's `DToS`). |
| **Error reply** | A reply whose key 0 holds a text. `NewError` builds one, `ErrorText` reads it; a transport status (HTTP, say) stays at success because the message is the protocol. |
| **Canonical message** | A fixed message whose bytes are pinned in the Go, C and Harbour tests, so the three implementations are proven to agree byte for byte. |

The package **encodes and decodes** — it does not own a server, a socket or
a schema. Keys and commands are constants the application declares on each
side (a `const` block in Go, a `.h` in C, a `.ch` in Harbour), or once in a
`.kvp` declaration that the `kvpack` command turns into code for the three.
The declaration is not part of the format: the bytes are the same either way.

---


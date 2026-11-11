---
mkskill:
  pos: 60
---

## Validate a message

```
kvpack validate api.kvp request.bin
kvpack validate -reply api.kvp reply.bin
```

The message is read as the command or record its number names, or with
`-reply` as that command's reply, and printed with the declared names:
every field with its value, `MISSING` where a generated decoder would fail,
optional fields that are absent, list elements of another type that would
be skipped, and keys that are not declared (ignored). An error reply prints
its text. The exit code is 1 when there is a problem.

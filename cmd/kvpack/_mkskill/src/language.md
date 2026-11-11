---
mkskill:
  pos: 40
---

## The .kvp language

```
# api.kvp
command login = 1
    user      text   = 1
    password  text   = 2
    when      ts     = 3   optional
reply login
    name      text   = 1
    events    event  = 3   list

record event = 100
    id        int32  = 1
    title     text   = 2
```

- One declaration per line; `#` starts a comment; indentation is only for
  the eye.
- `command name = number` and `record name = number` open a block; `reply
  name` opens the reply of a command and takes its number.
- A field is `name type = key [list | optional]`. Types: `int32`, `int64`,
  `ts`, `text`, `bin`, `bool`, `float64`, `ds` (a day, `yyyymmdd`), `msg` or
  the name of a record.
- Names are lowercase (`[a-z][a-z0-9_]*`); commands and records share names
  and numbers; keys are 1..65535 (0 is the error text) and unique in their
  block. `pack` and `unpack` are reserved field names; C keywords are
  refused when generating C.
- Fields are required unless `optional`; a `list` may be empty.

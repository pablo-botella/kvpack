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
- A field is `name type = key [list | optional] [nullable]`. Types: `int32`,
  `int64`, `ts`, `text`, `bin`, `bool`, `float64`, `ds` (a day, `yyyymmdd`),
  `msg`, `tupleset` or the name of a record.
- Names are lowercase (`[a-z][a-z0-9_]*`); commands and records share names
  and numbers; keys are 1..65535 (0 is the error text) and unique in their
  block. `pack` and `unpack` are reserved field names; C keywords are
  refused when generating C.
- Fields are required unless `optional`; a `list` may be empty.
- `nullable` lets a field arrive as a null instead of a value; without it a
  null is refused like any other wrong type. It does not go with `list`.
- The two modifiers decide when a field is written, and so what one that
  did not arrive means:

  | declared | written | when it does not arrive |
  |---|---|---|
  | (nothing) | always | the decode fails |
  | `optional` | when it is not empty (zero, no bytes) | it reads empty |
  | `nullable` | always, as its value or as a null | the decode fails |
  | `optional nullable` | when it is not null | it reads null |

  So `optional` leaves out the empty value and `optional nullable` leaves
  out the null: a field has two states at most, never three. An `optional
  nullable` at zero is written, because zero is not null.
- `tupleset` is data whose shape is only known when the message is built:
  a tuple definition and its tuples, all under the field's key. It declares
  no fields, takes `optional`, and neither `list` nor `nullable`.

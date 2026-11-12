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
  refused when generating C, and so are in Harbour the field and command
  names a class cannot take: `new`, `init`, `encode`, `decode`, `error`, `self`,
  `super`, `classname` and the like for both, and `handler`, `dispatch`,
  `transport` and `cerror` for a command, which are the multiplexer's and the
  client's own.
- `service name` and `include "file.kvp"` stand outside the blocks:

  ```
  # sqlbk.kvp
  service sqlbk
  include "sqlbk_types.kvp"

  command query = 1
      sql       text     = 1
  reply query
      results   result   = 1   list      # result is declared in sqlbk_types.kvp
  ```

  `service` names the service a file declares: its commands, one or more,
  grouped under a name. The name does not travel in the messages; it is what
  the generated names start from (the Go package, the prefix of the C and
  Harbour names) instead of the file name. `include` reads another file,
  its path relative to the file that includes it, into the same declaration:
  it is one namespace, so no name or number repeats across the files, and a
  type may be declared in any of them, in any order. A file is read once,
  however many times it is included. A file that declares a service is a
  declaration on its own and cannot be included; what is to be shared goes
  in a file that declares none.
- A `names` section says what an implementation calls what is generated,
  so the names are part of the declaration and not of how it is run:

  ```
  names                              # every language
      record   result = sqlbk_record
  names go                           # Go alone
      package         = sqlbk
      command  query  = sqlbk_query
  names c                            # C alone
      prefix          = sqlbk
      command  *      = call_sqlbk_*
      reply    *      = res_sqlbk_*
  ```

  The header is `names [lang [side]]`: `lang` is `go`, `c`, `harbour` or `*`,
  `side` is `server`, `client` or `*`, and what is not written is any. The
  most specific section wins. Inside, each line is what is named, `=`, its
  name:

  | line | names |
  |---|---|
  | `command <cmd> = name` | the type of the request of that command |
  | `reply <cmd> = name` | the type of its reply |
  | `record <rec> = name` | the type of a record |
  | `package = name` | the Go package (a `go` section) |
  | `prefix = name` | what precedes a C or Harbour type that has no name of its own |
  | `service = name` | the service itself on one side: in a `server` section the multiplexer, in a `client` section the client |

  `*` instead of a command or a record names them all, and stands in the
  name for each one's own (`command * = call_sqlbk_*` gives
  `call_sqlbk_query`); a line for the block itself wins over it. Names are
  written in lowercase with `_` and each generator turns them into its own
  convention (`sqlbk_query` is `SqlbkQuery` in Go and Harbour). A type name is
  the whole name: no prefix is added to it, and its functions follow it
  (`sqlbk_query_encode`). The constants of numbers and keys keep the prefix
  and the declared names. A type is named per language, in a section with no
  side: both sides of a language share the types. Field names have no alias.
  The side is for `service`: `names go server` with `service = sqlbk_service`
  calls the Go multiplexer `SqlbkService` (and its handler interface
  `SqlbkServiceHandler`), `names c client` with `service = sqlbk` gives the
  C client `sqlbk_client` and the calls `sqlbk_call_<command>`, and in Harbour
  it is the name of the class. Without it they are `Server` and `Client` in
  Go, and the prefix in C (`<prefix>_dispatch`, `<prefix>_client`) and Harbour
  (`<Prefix>Server`, `<Prefix>Client`).
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

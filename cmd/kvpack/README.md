# kvpack (CLI)

[![Go Reference](https://pkg.go.dev/badge/github.com/pablo-botella/kvpack/cmd/kvpack.svg)](https://pkg.go.dev/github.com/pablo-botella/kvpack/cmd/kvpack)

Command-line companion of [kvpack](https://pkg.go.dev/github.com/pablo-botella/kvpack):
it reads a `.kvp` declaration — the commands, replies and records of an
application, with named, typed, numbered fields — checks it, generates the
code that packs and unpacks those messages in **Go**, **C** and **Harbour**,
together with a multiplexer for the side that serves the commands and a
call per command for the side that asks, and validates message files
against it.

## Install

A ready binary for Windows, Linux and macOS is attached to every
[release](https://github.com/pablo-botella/kvpack/releases): no Go needed.
With Go:

```
go install github.com/pablo-botella/kvpack/cmd/kvpack@latest
```

## Commands

| Command | Description |
|---|---|
| `check file.kvp` | Read and check a declaration; print what it declares |
| `gen [flags] file.kvp` | Generate Go, C and/or Harbour code |
| `validate [-reply] file.kvp message.bin` | Check one message against the declaration and print it with names |
| `version` | Print the version (embedded release + build info) |
| `generate-claude-skill`, `generate-agent-docs` | Write the AI docs of kvpack |

`gen` flags (they go before the file):

| Flag | Description |
|---|---|
| `-go file` | Go code, package `-pkg`, and next to it `<package>_handler-go.txt`, the skeleton of the handler of the server side |
| `-pkg name` | Go package name (default: the one a `names` section of the `.kvp` gives, then its service, then its base name) |
| `-c file.h` | C header, and the `.c` of the same name next to it, and `<prefix>_handler-c.txt`, the skeleton of the handler |
| `-hb file.prg` | Harbour classes, the `.ch` of the same name next to them, and `<prefix>_handler-prg.txt`: the skeleton of the handler of the server side, to copy and fill |
| `-prefix p` | Prefix of the C and Harbour names (default: the one a `names` section of the `.kvp` gives, then its service, then its base name) |

A file whose content would not change is not rewritten, so builds that
look at timestamps stay quiet.

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

## Generate code

```
kvpack gen -go api/api_kvp.go -pkg api -c client/api_kvp.h -hb hb/api_kvp.prg api.kvp
```

In a Go project it fits a `//go:generate` line. Every generated file starts
with `Code generated by kvpack ... DO NOT EDIT.`: change the `.kvp` and
regenerate instead.

The generated code applies the same rules in the three languages: a
required field missing, or present with another type, fails the decode
(Go: an error naming it; C and Harbour: `KVP_E_MISSING`); a reply carrying
key 0 is an error reply (Go: the error is its text; C and Harbour:
`KVP_E_REPLY`); optional fields read as zero when absent and are not
written when zero (a tupleset: when it has no fields); a field nobody set
goes as its empty value, the same bytes in the three languages; an
`optional nullable` field is not written when null
and reads null when absent; unknown keys are ignored. Single fields are
written first and lists after them, so the three produce the same bytes.

A declaration with commands gives more than its types, in the same files:
a **multiplexer** for the side that serves (it finds the command of a
message, unpacks the request and calls the application's handler,
`On<Command>`) and a **call per command** for the side that asks (it packs
the request, hands it to whatever carries it and unpacks the reply).
Neither has a transport in it. Next to the code goes the skeleton of the
handler, to copy and fill: `<package>_handler-go.txt`,
`<prefix>_handler-c.txt` and `<prefix>_handler-prg.txt`; it is written again
on every run and is no part of the build.

The C code is built on `kvp_arena.h` (with `dyn_buffer.h` under it), next
to `kvpack.h` in `capi/`: compile `kvpack.c`, `dyn_buffer.c` and
`kvp_arena.c` with the generated `.c`.

What each language gets is described in the
[kvpack README](https://github.com/pablo-botella/kvpack#declarations-and-generated-code).

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

## See also

- [kvpack](https://github.com/pablo-botella/kvpack#readme) — the format, the
  Go package, the C and Harbour writer/reader, and the generated code

## License

MIT

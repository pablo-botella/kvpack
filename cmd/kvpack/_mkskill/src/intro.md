---
mkskill:
  pos: 10
---

# kvpack (CLI)

[![Go Reference](https://pkg.go.dev/badge/github.com/pablo-botella/kvpack/cmd/kvpack.svg)](https://pkg.go.dev/github.com/pablo-botella/kvpack/cmd/kvpack)

Command-line companion of [kvpack](https://pkg.go.dev/github.com/pablo-botella/kvpack):
it reads a `.kvp` declaration — the commands, replies and records of an
application, with named, typed, numbered fields — checks it, generates the
code that packs and unpacks those messages in **Go**, **C** and **Harbour**,
together with a multiplexer for the side that serves the commands and a
call per command for the side that asks, and validates message files
against it.

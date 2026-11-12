---
mkskill:
  pos: 30
---

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

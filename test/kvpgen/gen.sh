#!/bin/sh
# gen.sh - generates the code of the test service with the kvpack command,
# as an application would for its own declaration.
#
# api.kvp declares the service and includes the other files of it
# (api_records.kvp, api_query.kvp, api_every.kvp); the command is given that
# one file. It declares "service api", so nothing here names the Go package
# or the prefix of the C and Harbour names: they come from the service.
#
# What it writes:
#   -go   api/api_kvp.go               the Go code: types, multiplexer, client
#         api/api_handler-go.txt       the skeleton of a handler, to copy
#   -c    c/api_kvp.h, c/api_kvp.c     the C code
#         c/api_handler-c.txt
#   -hb   hb/api_kvp.prg, hb/api_kvp.ch   the Harbour code
#         hb/api_handler-prg.txt
#
# Run it after changing a .kvp or the generator: the tests compare these
# files with what the generator gives and fail when they are stale.
#
#   sh gen.sh
set -e
cd "$(dirname "$0")"

go run ../../cmd/kvpack check api.kvp
go run ../../cmd/kvpack gen -go api/api_kvp.go -c c/api_kvp.h -hb hb/api_kvp.prg api.kvp

echo "generated: api/api_kvp.go  c/api_kvp.h  c/api_kvp.c  hb/api_kvp.prg  hb/api_kvp.ch"
echo "skeletons: api/api_handler-go.txt  c/api_handler-c.txt  hb/api_handler-prg.txt"

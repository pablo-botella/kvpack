@echo off
rem gen.bat - generates the code of the test service with the kvpack command,
rem as an application would for its own declaration.
rem
rem api.kvp declares the service and includes the other files of it
rem (api_records.kvp, api_query.kvp, api_every.kvp); the command is given that
rem one file. It declares "service api", so nothing here names the Go package
rem or the prefix of the C and Harbour names: they come from the service.
rem
rem What it writes:
rem   -go   api\api_kvp.go               the Go code: types, multiplexer, client
rem         api\api_handler-go.txt       the skeleton of a handler, to copy
rem   -c    c\api_kvp.h, c\api_kvp.c     the C code
rem         c\api_handler-c.txt
rem   -hb   hb\api_kvp.prg, hb\api_kvp.ch   the Harbour code
rem         hb\api_handler-prg.txt
rem
rem Run it after changing a .kvp or the generator: the tests compare these
rem files with what the generator gives and fail when they are stale.
setlocal
cd /d "%~dp0"

go run ..\..\cmd\kvpack check api.kvp
if errorlevel 1 exit /b 1

go run ..\..\cmd\kvpack gen -go api\api_kvp.go -c c\api_kvp.h -hb hb\api_kvp.prg api.kvp
if errorlevel 1 exit /b 1

echo generated: api\api_kvp.go  c\api_kvp.h  c\api_kvp.c  hb\api_kvp.prg  hb\api_kvp.ch
echo skeletons: api\api_handler-go.txt  c\api_handler-c.txt  hb\api_handler-prg.txt

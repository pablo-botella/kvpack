@echo off
rem build.bat - builds and runs the test of the generated C code with MSVC,
rem with the flags of a strict consumer (/W4 /std:c11 /utf-8 /MT), 32-bit by
rem default. Pass "x64" for 64-bit. Requires Visual Studio 2022 or newer.
setlocal

set ARCH=32
if /i "%~1"=="x64" set ARCH=64

set VSROOT=
for %%d in ("%ProgramFiles%\Microsoft Visual Studio\2022\Professional" ^
            "%ProgramFiles%\Microsoft Visual Studio\2022\Community" ^
            "%ProgramFiles%\Microsoft Visual Studio\2022\Enterprise" ^
            "%ProgramFiles%\Microsoft Visual Studio\2022\BuildTools" ^
            "%ProgramFiles(x86)%\Microsoft Visual Studio\2022\BuildTools") do (
    if exist "%%~d\VC\Auxiliary\Build\vcvars%ARCH%.bat" if not defined VSROOT set "VSROOT=%%~d"
)
if not defined VSROOT (
    echo build.bat: Visual Studio 2022 not found
    exit /b 1
)

call "%VSROOT%\VC\Auxiliary\Build\vcvars%ARCH%.bat" >nul
if errorlevel 1 exit /b 1

cd /d "%~dp0"
cl /nologo /W4 /WX /O2 /MT /std:c11 /utf-8 /I..\..\..\capi /Fe:test_api.exe ..\..\..\capi\kvpack.c ..\..\..\capi\dyn_buffer.c ..\..\..\capi\kvp_arena.c api_kvp.c test_api.c
set RC=%errorlevel%
del /q *.obj 2>nul
if %RC% neq 0 exit /b %RC%
.\test_api.exe

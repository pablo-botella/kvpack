@echo off
rem build.bat - builds and runs the C tests with MSVC, 32-bit by default so it
rem matches the programs the reader is meant for. Pass "x64" for 64-bit.
rem Requires Visual Studio 2022 Build Tools or newer.
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
cl /nologo /W4 /O2 /MT /std:c11 /utf-8 /Fe:test_kvpack.exe kvpack.c test_kvpack.c
set RC=%errorlevel%
if %RC% equ 0 cl /nologo /W4 /WX /O2 /MT /std:c11 /utf-8 /Fe:test_dyn_buffer.exe kvpack.c dyn_buffer.c kvp_arena.c test_dyn_buffer.c
if %RC% equ 0 set RC=%errorlevel%
del /q *.obj 2>nul
if %RC% neq 0 exit /b %RC%
.\test_kvpack.exe
if errorlevel 1 exit /b 1
.\test_dyn_buffer.exe

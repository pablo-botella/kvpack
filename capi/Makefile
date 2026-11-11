# Makefile - builds and runs the C test with gcc/MinGW or clang.
#   make                64-bit (or whatever the compiler defaults to)
#   make CFLAGS+=-m32   32-bit, when the toolchain supports it
CC      = gcc
CFLAGS  ?= -std=c89 -pedantic -Wno-long-long -Wall -Wextra -O2
LDFLAGS ?=
EXE     := test_kvpack$(if $(findstring Windows_NT,$(OS)),.exe,)

all: $(EXE)
	./$(EXE)

$(EXE): kvpack.c test_kvpack.c kvpack.h
	$(CC) $(CFLAGS) -o $@ kvpack.c test_kvpack.c $(LDFLAGS)

clean:
	rm -f $(EXE) *.o *.obj

.PHONY: all clean

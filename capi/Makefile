# Makefile - builds and runs the C tests with gcc/MinGW or clang.
#   make                64-bit (or whatever the compiler defaults to)
#   make CFLAGS+=-m32   32-bit, when the toolchain supports it
CC      = gcc
CFLAGS  ?= -std=c89 -pedantic -Wno-long-long -Wall -Wextra -O2
LDFLAGS ?=
X       := $(if $(findstring Windows_NT,$(OS)),.exe,)
EXE     := test_kvpack$(X)
EXE_BUF := test_dyn_buffer$(X)

all: $(EXE) $(EXE_BUF)
	./$(EXE)
	./$(EXE_BUF)

$(EXE): kvpack.c test_kvpack.c kvpack.h
	$(CC) $(CFLAGS) -o $@ kvpack.c test_kvpack.c $(LDFLAGS)

# the growing buffer and the arena built on it (optional pieces: kvpack.c
# needs neither)
$(EXE_BUF): kvpack.c dyn_buffer.c kvp_arena.c test_dyn_buffer.c kvpack.h dyn_buffer.h kvp_arena.h
	$(CC) $(CFLAGS) -o $@ kvpack.c dyn_buffer.c kvp_arena.c test_dyn_buffer.c $(LDFLAGS)

clean:
	rm -f $(EXE) $(EXE_BUF) *.o *.obj

.PHONY: all clean

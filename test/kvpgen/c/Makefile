# Makefile - builds and runs the test of the generated C code with gcc/MinGW
# or clang, as strict C89.
#   make                64-bit (or whatever the compiler defaults to)
#   make CFLAGS+=-m32   32-bit, when the toolchain supports it
CC      = gcc
CAPI    = ../../../capi
CFLAGS  ?= -std=c89 -pedantic -Wno-long-long -Wall -Wextra -Werror -O2
LDFLAGS ?=
EXE     := test_api$(if $(findstring Windows_NT,$(OS)),.exe,)

all: $(EXE)
	./$(EXE)

$(EXE): $(CAPI)/kvpack.c api_kvp.c test_api.c api_kvp.h $(CAPI)/kvpack.h
	$(CC) $(CFLAGS) -I$(CAPI) -o $@ $(CAPI)/kvpack.c api_kvp.c test_api.c $(LDFLAGS)

clean:
	rm -f $(EXE) *.o *.obj

.PHONY: all clean

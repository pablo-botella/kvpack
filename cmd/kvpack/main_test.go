package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pablo-botella/kvpack"
)

const apiKvp = "../../test/kvpgen/api.kvp"

func reply() *kvpack.Message {
	return kvpack.New(1).AddText(1, "ann").AddTime(2, time.Unix(1700000000, 0)).
		AddMessage(5, kvpack.New(101).AddText(1, "di")).
		AddMessage(4, kvpack.New(100).AddInt32(1, 7).AddText(2, "a").AddMessage(7, kvpack.New(101).AddText(1, "bo")).AddInt32(6, 1))
}

func runValidate(t *testing.T, m *kvpack.Message, asReply bool) (string, int, error) {
	t.Helper()
	f, err := load(apiKvp)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	n, err := validate(&out, f, m.Encode(), asReply)
	return out.String(), n, err
}

func TestValidateGood(t *testing.T) {
	out, n, err := runValidate(t, reply(), true)
	if err != nil || n != 0 {
		t.Fatalf("%d %v\n%s", n, err, out)
	}
	for _, want := range []string{"reply login (1), 4 items", `name`, `"ann"`, "2023-11-14T22:13:20Z", "absent (optional)", "record event (100)", "[0] 1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestValidateProblems(t *testing.T) {
	// password of another type, plus an undeclared key
	m := kvpack.New(1).AddText(1, "bob").AddInt32(2, 5).AddText(99, "x")
	out, n, err := runValidate(t, m, false)
	if err != nil || n != 1 {
		t.Fatalf("%d %v\n%s", n, err, out)
	}
	if !strings.Contains(out, "MISSING: found int32, not text") || !strings.Contains(out, "key 99 text: not declared") {
		t.Errorf("%s", out)
	}
	// nested record missing a required field
	bad := kvpack.New(1).AddText(1, "ann").AddTime(2, time.Unix(1, 0)).AddMessage(4, kvpack.New(100).AddInt32(1, 7))
	if out, n, _ := runValidate(t, bad, true); n != 2 { // title and who
		t.Errorf("nested: %d\n%s", n, out)
	}
}

func TestValidateErrorReplyAndUnknown(t *testing.T) {
	out, n, err := runValidate(t, kvpack.NewError(1, "nope"), true)
	if err != nil || n != 0 || !strings.Contains(out, `error reply: "nope"`) {
		t.Fatalf("%d %v %s", n, err, out)
	}
	if _, _, err := runValidate(t, kvpack.New(42), false); err == nil {
		t.Error("undeclared command accepted")
	}
	f, _ := load(apiKvp)
	if _, err := validate(&bytes.Buffer{}, f, []byte("junk"), false); err == nil {
		t.Error("junk accepted")
	}
}

func TestGen(t *testing.T) {
	dir := t.TempDir()
	err := run([]string{"gen", "-go", filepath.Join(dir, "a.go"), "-c", filepath.Join(dir, "a.h"), "-hb", filepath.Join(dir, "a.prg"), apiKvp})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.go", "a.h", "a.c", "a.prg", "a.ch"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || !bytes.Contains(b, []byte("DO NOT EDIT")) {
			t.Errorf("%s: %v", f, err)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.go")); !bytes.Contains(b, []byte("package api")) {
		t.Error("default package is not the .kvp name")
	}
	if err := run([]string{"gen", apiKvp}); err == nil {
		t.Error("gen with nothing to write accepted")
	}
}

func TestDefaultName(t *testing.T) {
	for in, want := range map[string]string{"api.kvp": "api", "dir/My-API.kvp": "my_api", "1st.kvp": "kvp_1st"} {
		if got := defaultName(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

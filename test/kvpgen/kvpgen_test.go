package kvpgen_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pablo-botella/kvpack"
	"github.com/pablo-botella/kvpack/internal/gen"
	"github.com/pablo-botella/kvpack/internal/schema"
	"github.com/pablo-botella/kvpack/test/kvpgen/api"
)

// canonicalReplyHex is the reply the Go, C (c/test_api.c) and Harbour
// (hb/test_api.prg) generated code must all write and read: a change in one
// generator that alters these bytes breaks the other two tests on purpose.
const canonicalReplyHex = "4b5650010100000005000000400100000c0f37fa0100040003000000616e6e020003000800000000f1536500000000050006001e0000004b56500165000000010000000a000000e6db35f00100040002000000646904000600670000004b56500164000000050000005300000022dac938010001000400000007000000020004000100000061070006001e0000004b56500165000000010000000a00000055d90c4f0100040002000000626f06000100040000000100000006000100040000000200000004000600880000004b565001640000000700000074000000411e320d01000100040000000800000002000400010000006203000200080000000500000000000000070006001e0000004b56500165000000010000000a000000455dc3a201000400020000006379080007000100000001090008000800000000000000000004400a000900080000003230323631313131"

func canonicalReply() api.LoginReply {
	return api.LoginReply{
		Name:    "ann",
		Expires: time.Unix(1700000000, 0).UTC(),
		Events: []api.Event{
			{ID: 7, Title: "a", Codes: []int32{1, 2}, Who: &api.Person{Name: "bo"}},
			{ID: 8, Title: "b", Big: 5, Who: &api.Person{Name: "cy"},
				Active: true, Score: 2.5, Day: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)},
		},
		Owner: &api.Person{Name: "di"},
	}
}

func load(t *testing.T) *schema.File {
	t.Helper()
	fh, err := os.Open("api.kvp")
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	f, err := schema.Parse("api.kvp", fh)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// TestGeneratedFresh is the golden guard of the generated files the tests
// compile: regenerate with
//
//	go run ./cmd/kvpack gen -go test/kvpgen/api/api_kvp.go -pkg api -c test/kvpgen/c/api_kvp.h -hb test/kvpgen/hb/api_kvp.prg test/kvpgen/api.kvp
func TestGeneratedFresh(t *testing.T) {
	f := load(t)
	goSrc, err := gen.Go(f, "api")
	if err != nil {
		t.Fatal(err)
	}
	h, c, err := gen.C(f, "api", "c/api_kvp.h")
	if err != nil {
		t.Fatal(err)
	}
	ch, prg, err := gen.Harbour(f, "api", "hb/api_kvp.ch")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{
		"api/api_kvp.go": goSrc, "c/api_kvp.h": h, "c/api_kvp.c": c, "hb/api_kvp.ch": ch, "hb/api_kvp.prg": prg,
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
			t.Errorf("%s is stale: regenerate it (see the comment of this test)", path)
		}
	}
}

func TestCanonicalReply(t *testing.T) {
	r := canonicalReply()
	got := hex.EncodeToString(r.Pack().Encode())
	if got != canonicalReplyHex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonicalReplyHex)
	}
	b, _ := hex.DecodeString(canonicalReplyHex)
	m, err := kvpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	var back api.LoginReply
	if err := back.Unpack(m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, r) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, r)
	}
}

func TestOptionalLeftOut(t *testing.T) {
	m := (&api.Login{User: "bob", Password: "x"}).Pack()
	if m.Find(3) != nil || len(m.Items) != 2 {
		t.Fatalf("zero optional written: %+v", m.Items)
	}
	var l api.Login
	if err := l.Unpack(m); err != nil || !l.When.IsZero() || l.Tags != nil {
		t.Fatalf("%v %+v", err, l)
	}
}

func TestMissing(t *testing.T) {
	cases := []struct {
		name string
		m    *kvpack.Message
		want string
	}{
		{"absent", kvpack.New(api.CmdLogin).AddText(1, "bob"), "login: missing password"},
		{"wrong type", kvpack.New(api.CmdLogin).AddText(1, "bob").AddInt32(2, 5), "login: missing password"},
		{"nested", kvpack.New(api.CmdLogin).AddText(1, "ann").AddTime(2, time.Unix(1, 0)).
			AddMessage(4, kvpack.New(api.RecEvent).AddInt32(1, 7).AddText(2, "a")), "login_reply: events: event: missing who"},
	}
	for _, c := range cases {
		var err error
		if c.name == "nested" {
			var r api.LoginReply
			err = r.Unpack(c.m)
		} else {
			var l api.Login
			err = l.Unpack(c.m)
		}
		if err == nil || err.Error() != c.want {
			t.Errorf("%s: %v, want %q", c.name, err, c.want)
		}
	}
}

func TestErrorReply(t *testing.T) {
	var r api.LoginReply
	if err := r.Unpack(kvpack.NewError(api.CmdLogin, "wrong password")); err == nil || err.Error() != "wrong password" {
		t.Fatalf("%v", err)
	}
}

func TestUnknownKeysIgnored(t *testing.T) {
	m := (&api.Login{User: "bob", Password: "x"}).Pack().AddText(99, "new field").AddInt32(4, 3)
	var l api.Login
	if err := l.Unpack(m); err != nil || l.User != "bob" || l.Tags != nil {
		t.Fatalf("%v %+v", err, l)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"user text = 1", "outside a command"},
		{"command a = 1\ncommand b = 1", "number 1 already used"},
		{"command a = 1\nrecord a = 2", "name already used"},
		{"command a = 1\n x text = 1\n y text = 1", "key 1 already used"},
		{"command a = 1\n x text = 0", "must be 1..65535"},
		{"command a = 1\n x text = 1\n x int32 = 2", "already declared"},
		{"command a = 1\n x thing = 1", "unknown type"},
		{"command a = 1\n x text = 1 list optional", "already optional"},
		{"command a = 1\n x text = 1 maybe", "unknown modifier"},
		{"reply a", "no command a"},
		{"command a = 1\nreply a\nreply a", "already declared"},
		{"command Login = 1", "lowercase"},
		{"command a = 1\n pack text = 1", "reserved"},
		{"command a = 1\nrecord a_reply = 2\nreply a", "collides"},
		{"command a 1", "expected"},
	}
	for _, c := range cases {
		_, err := schema.Parse("t.kvp", strings.NewReader(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.src, err, c.want)
		}
	}
}

func TestCErrors(t *testing.T) {
	f, err := schema.Parse("t.kvp", strings.NewReader("record a = 1\n self a = 1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := gen.C(f, "t", "t.h"); err == nil || !strings.Contains(err.Error(), "contains itself") {
		t.Errorf("self record: %v", err)
	}
	f, _ = schema.Parse("t.kvp", strings.NewReader("command a = 1\n int int32 = 1"))
	if _, _, err := gen.C(f, "t", "t.h"); err == nil || !strings.Contains(err.Error(), "C keyword") {
		t.Errorf("keyword: %v", err)
	}
}

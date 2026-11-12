package kvpgen_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// load reads the test service: api.kvp and the files it includes.
func load(t *testing.T) *schema.File {
	t.Helper()
	f, err := schema.ParseFile("api.kvp")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// The test service is a declaration over several files: what the three
// languages compile and run comes from includes, a service and a type that
// a names section renamed.
func TestServiceOfTheTests(t *testing.T) {
	f := load(t)
	if f.Service != "api" || len(f.Commands()) != 4 || len(f.Replies()) != 2 || len(f.Records()) != 2 {
		t.Fatalf("service %q: %d commands, %d replies, %d records", f.Service, len(f.Commands()), len(f.Replies()), len(f.Records()))
	}
	from := map[string]string{}
	for _, b := range f.Blocks {
		if b.Kind != schema.Reply {
			from[b.Name] = b.File
		}
	}
	for name, file := range map[string]string{
		"login": "api.kvp", "ping": "api.kvp", "event": "api_records.kvp", "person": "api_records.kvp",
		"query": "api_query.kvp", "every": "api_every.kvp",
	} {
		if from[name] != file {
			t.Errorf("%s: read from %q, want %q", name, from[name], file)
		}
	}
	for _, b := range f.Commands() {
		if b.Name != "ping" {
			continue
		}
		if g, c, h := f.TypeName(schema.Go, b), f.TypeName(schema.C, b), f.TypeName(schema.Harbour, b); g != "heartbeat" || c != "api_heartbeat" || h != "api_heartbeat" {
			t.Errorf("the request of ping is named %q, %q, %q", g, c, h)
		}
	}
}

// TestGeneratedFresh guards the generated files the tests compile: it
// generates them again in memory and compares. Regenerate them with gen.bat,
// or gen.sh, in this folder, which run
//
//	kvpack gen -go api/api_kvp.go -c c/api_kvp.h -hb hb/api_kvp.prg api.kvp
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
	skel, err := gen.HarbourHandler(f, "api", "hb/api_kvp.ch")
	if err != nil {
		t.Fatal(err)
	}
	goSkel, err := gen.GoHandler(f, "api")
	if err != nil {
		t.Fatal(err)
	}
	cSkel, err := gen.CHandler(f, "api", "c/api_kvp.h")
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{
		"api/api_kvp.go": goSrc, "c/api_kvp.h": h, "c/api_kvp.c": c, "hb/api_kvp.ch": ch, "hb/api_kvp.prg": prg,
		"hb/api_handler-prg.txt": skel, "api/api_handler-go.txt": goSkel, "c/api_handler-c.txt": cSkel,
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
		{"command a = 1\n x text = 1 list nullable", "cannot be nullable"},
		{"command a = 1\n x tupleset = 1 list", "already a list"},
		{"command a = 1\n x tupleset = 1 nullable", "cannot be nullable"},
		{"command a = 1\n x text = 1 nullable\n x_null bool = 2", "collides with the null flag"},
		{"command tupleset = 1", "the name is a type"},

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

// canonicalQueryHex is the query reply the three generated codes must write
// and read: a nullable field sent as a null, an optional nullable one that
// is null and so left out, a record, and a tupleset (id int32, name text)
// with the tuples 1, "Ana" and 2, null.
const canonicalQueryHex = "4b5650010300000006000000930000000704f72e01000100040000000700000002000a0000000000040006001e0000004b56500165000000010000000a000000e6db35f00100040002000000646905000b001e000000020000000100020000006964000000000400040000006e616d650000000005000c001300000001000400000001000000040003000000416e6105000c0010000000010004000000020000000a0000000000"

func canonicalQuery() api.QueryReply {
	rows := kvpack.NewTupleSet(kvpack.NewTupleDef().Field("id", kvpack.Int32).Field("name", kvpack.Text))
	rows.Add(int32(1), "Ana")
	rows.Add(int32(2), nil)
	return api.QueryReply{Code: 7, NoteNull: true, LimitNull: true, Author: &api.Person{Name: "di"}, Rows: rows}
}

func TestCanonicalQuery(t *testing.T) {
	r := canonicalQuery()
	got := hex.EncodeToString(r.Pack().Encode())
	if got != canonicalQueryHex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonicalQueryHex)
	}
	b, _ := hex.DecodeString(canonicalQueryHex)
	m, err := kvpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	var back api.QueryReply
	if err := back.Unpack(m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, r) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, r)
	}
}

func TestNullable(t *testing.T) {
	rows := kvpack.NewTupleSet(kvpack.NewTupleDef().Field("n", kvpack.Int32))
	base := func() *kvpack.Message { return kvpack.New(api.CmdQuery).AddInt32(1, 0) }
	person := kvpack.New(api.RecPerson).AddText(1, "di")

	// a value where a null may come; an optional nullable that does not
	// arrive reads null
	var r api.QueryReply
	if err := r.Unpack(base().AddText(2, "hi").AddMessage(4, person).AddTupleSet(5, rows)); err != nil {
		t.Fatal(err)
	}
	if r.Note != "hi" || r.NoteNull || !r.LimitNull || r.Limit != 0 || r.AuthorNull || r.Author.Name != "di" {
		t.Errorf("%+v", r)
	}
	// and one that arrives, even as a zero, is not null
	if err := r.Unpack(base().AddText(2, "").AddInt32(3, 0).AddMessage(4, person).AddTupleSet(5, rows)); err != nil {
		t.Fatal(err)
	}
	if r.NoteNull || r.LimitNull || r.Limit != 0 {
		t.Errorf("%+v", r)
	}
	if r.Rows == nil || len(r.Rows.Def.Fields) != 1 || len(r.Rows.Rows) != 0 || r.Extra != nil {
		t.Errorf("tuplesets: %+v %+v", r.Rows, r.Extra)
	}
	// nulls everywhere they may come
	if err := r.Unpack(base().AddNull(2).AddNull(3).AddNull(4).AddTupleSet(5, rows).AddTupleSet(6, rows)); err != nil {
		t.Fatal(err)
	}
	if !r.NoteNull || !r.LimitNull || !r.AuthorNull || r.Author != nil || r.Extra == nil {
		t.Errorf("%+v", r)
	}
	// the Null flag wins over the value; an optional nullable is left out
	// when null and written when not, whatever its value
	m := (&api.QueryReply{Note: "ignored", NoteNull: true, Limit: 5, LimitNull: true, AuthorNull: true, Rows: rows}).Pack()
	if !m.IsNull(2) || m.Find(3) != nil || !m.IsNull(4) {
		t.Errorf("%+v", m.Items)
	}
	m = (&api.QueryReply{Rows: rows}).Pack()
	if v, ok := m.Int32(3); !ok || v != 0 {
		t.Errorf("an optional nullable at zero is not null: %+v", m.Items)
	}
	if v, ok := m.Text(2); !ok || v != "" || m.IsNull(2) {
		t.Errorf("a nullable at zero is an empty value: %+v", m.Items)
	}
	// what is not nullable still refuses a null, and a required one must come
	for name, c := range map[string]struct {
		m    *kvpack.Message
		want string
	}{
		"null code":    {kvpack.New(api.CmdQuery).AddNull(1).AddNull(2).AddNull(4).AddTupleSet(5, rows), "query_reply: missing code"},
		"note absent":  {base().AddNull(4).AddTupleSet(5, rows), "query_reply: missing note"},
		"rows absent":  {base().AddNull(2).AddNull(4), "query_reply: missing rows"},
		"rows is text": {base().AddNull(2).AddNull(4).AddText(5, "x"), "query_reply: missing rows"},
	} {
		if err := r.Unpack(c.m); err == nil || err.Error() != c.want {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// Command every declares each type in each form a field can take: required
// (r_), optional (o_), nullable (n_), optional nullable (on_) and list (l_),
// plus a required and an optional tupleset. Three messages of it are pinned,
// the same in the C and Harbour tests:
//
//	full   every field with a value, two elements per list, a tupleset with
//	       a field of each type, one tuple with values and one all null
//	zeros  what a struct nobody touched writes, the same in the three
//	       languages: every field that always travels as its empty value (a
//	       zero, no bytes, a message of command 0 and no items, a record of
//	       empty fields, a tuple definition of no fields), nothing of the
//	       optional fields nor the lists
//	nulls  the same with every field that can be null set to null: the
//	       nullable ones go as nulls, the optional nullable ones are left out
const (
	everyFullHex  = "4b56500104000000400000003b0500007860084f0100010004000000ffffffff02000200080000000000000000010000030003000800000000f153650000000004000400040000007465787405000500030000000001ff060007000100000001070008000800000000000000000004400800090008000000323032363131313209000600140000004b565001090000000000000000000000000000000a0006001d0000004b5650016500000001000000090000005e1d56860100040001000000720b000100040000000b0000000c000200080000000c000000000000000d000300080000000df15365000000000e000400010000006f0f000500010000000f100007000100000001110008000800000000000000008031401200090008000000323032363031313813000600140000004b56500113000000000000000000000000000000140006001d0000004b565001650000000100000009000000877150e501000400010000006f15000100040000001500000016000200080000001600000000000000170003000800000017f153650000000018000400010000006e1900050001000000191a00070001000000011b000800080000000000000000803b401c0009000800000032303236303132381d000600140000004b5650011d0000000000000000000000000000001e0006001d0000004b5650016500000001000000090000001141579201000400010000006e1f000100040000001f00000020000200080000002000000000000000210003000800000021f153650000000022000400020000006f6e23000500010000002324000700010000000125000800080000000000000000c042402600090008000000323032363032313827000600140000004b56500127000000000000000000000000000000280006001e0000004b56500165000000010000000a0000008e97a58d01000400020000006f6e2900010004000000010000002900010004000000feffffff2a0002000800000003000000000000002a00020008000000fcffffffffffffff2b000300080000002bf15365000000002b0003000800000000000000000000002c00040001000000612c000400000000002d0005000200000001022d000500000000002e00070001000000012e00070001000000002f00080008000000000000000000e03f2f00080008000000000000000000f8bf300009000800000032303236303130313000090008000000202020202020202031000600140000004b5650010100000000000000000000000000000031000600140000004b56500102000000000000000000000000000000320006001d0000004b56500165000000010000000900000040f48366010004000100000078320006001d0000004b565001650000000100000009000000d6c4841101000400010000007933000b006d0000000900000001000100000069000000000200010000006a00000000030001000000740000000004000100000073060000006c656e3d343005000100000062000000000600010000006d000000000700010000006b000000000800010000006600000000090001000000640000000033000c007300000001000400000001000000020008000000020000000000000003000800000003f1536500000000040003000000416e61050001000000040600140000004b56500105000000000000000000000000000000070001000000010800080000000000000000001a40090008000000323032363037303733000c00360000000a00000000000a00000000000a00000000000a00000000000a00000000000a00000000000a00000000000a00000000000a000000000034000b000f000000010000000100010000006e00000000"
	everyNullsHex = "4b565001040000001500000001010000ecb25ee1010001000400000000000000020002000800000000000000000000000300030008000000000000000000000004000400000000000500050000000000060007000100000000070008000800000000000000000000000800090008000000202020202020202009000600140000004b565001000000000000000000000000000000000a0006001c0000004b565001650000000100000008000000e19d1932010004000000000015000a000000000016000a000000000017000a000000000018000a000000000019000a00000000001a000a00000000001b000a00000000001c000a00000000001d000a00000000001e000a000000000033000b000400000000000000"
	everyZerosHex = "4b565001040000001f000000fb010000d3e7d1a4010001000400000000000000020002000800000000000000000000000300030008000000000000000000000004000400000000000500050000000000060007000100000000070008000800000000000000000000000800090008000000202020202020202009000600140000004b565001000000000000000000000000000000000a0006001c0000004b565001650000000100000008000000e19d193201000400000000001500010004000000000000001600020008000000000000000000000017000300080000000000000000000000180004000000000019000500000000001a00070001000000001b0008000800000000000000000000001c0009000800000020202020202020201d000600140000004b565001000000000000000000000000000000001e0006001c0000004b565001650000000100000008000000e19d193201000400000000001f0001000400000000000000200002000800000000000000000000002100030008000000000000000000000022000400000000002300050000000000240007000100000000250008000800000000000000000000002600090008000000202020202020202027000600140000004b56500100000000000000000000000000000000280006001c0000004b565001650000000100000008000000e19d1932010004000000000033000b000400000000000000"
)

func ts(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

func day(y, m, d int) time.Time { return time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC) }

func everyFull() api.Every {
	set := kvpack.NewTupleSet(kvpack.NewTupleDef().
		Field("i", kvpack.Int32).Field("j", kvpack.Int64).Field("t", kvpack.Time).
		FieldMeta("s", kvpack.Text, []byte("len=40")).Field("b", kvpack.Bin).Field("m", kvpack.Msg).
		Field("k", kvpack.Bool).Field("f", kvpack.Float64).Field("d", kvpack.Date))
	set.Add(int32(1), int64(2), ts(1700000003), "Ana", []byte{4}, kvpack.New(5), true, 6.5, day(2026, 7, 7))
	set.Add(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	return api.Every{
		RInt32: -1, RInt64: 1 << 40, RTs: ts(1700000000), RText: "text", RBin: []byte{0, 1, 255},
		RBool: true, RFloat64: 2.5, RDs: day(2026, 11, 12), RMsg: kvpack.New(9), RRec: &api.Person{Name: "r"},

		OInt32: 11, OInt64: 12, OTs: ts(1700000013), OText: "o", OBin: []byte{15},
		OBool: true, OFloat64: 17.5, ODs: day(2026, 1, 18), OMsg: kvpack.New(19), ORec: &api.Person{Name: "o"},

		NInt32: 21, NInt64: 22, NTs: ts(1700000023), NText: "n", NBin: []byte{25},
		NBool: true, NFloat64: 27.5, NDs: day(2026, 1, 28), NMsg: kvpack.New(29), NRec: &api.Person{Name: "n"},

		OnInt32: 31, OnInt64: 32, OnTs: ts(1700000033), OnText: "on", OnBin: []byte{35},
		OnBool: true, OnFloat64: 37.5, OnDs: day(2026, 2, 18), OnMsg: kvpack.New(39), OnRec: &api.Person{Name: "on"},

		LInt32: []int32{1, -2}, LInt64: []int64{3, -4}, LTs: []time.Time{ts(1700000043), ts(0)},
		LText: []string{"a", ""}, LBin: [][]byte{{1, 2}, {}}, LBool: []bool{true, false},
		LFloat64: []float64{0.5, -1.5}, LDs: []time.Time{day(2026, 1, 1), {}},
		LMsg: []*kvpack.Message{kvpack.New(1), kvpack.New(2)}, LRec: []api.Person{{Name: "x"}, {Name: "y"}},

		RSet: set,
		OSet: kvpack.NewTupleSet(kvpack.NewTupleDef().Field("n", kvpack.Int32)),
	}
}

// everyZeros is a struct nobody touched: nothing null, everything empty.
func everyZeros() api.Every {
	return api.Every{}
}

// everyNulls is everyZeros with every field that can be null set to null.
func everyNulls() api.Every {
	e := everyZeros()
	e.NInt32Null, e.NInt64Null, e.NTsNull, e.NTextNull, e.NBinNull = true, true, true, true, true
	e.NBoolNull, e.NFloat64Null, e.NDsNull, e.NMsgNull, e.NRecNull = true, true, true, true, true
	e.OnInt32Null, e.OnInt64Null, e.OnTsNull, e.OnTextNull, e.OnBinNull = true, true, true, true, true
	e.OnBoolNull, e.OnFloat64Null, e.OnDsNull, e.OnMsgNull, e.OnRecNull = true, true, true, true, true
	return e
}

// keysOf lists the keys of a message, in order, a null as its negative.
func keysOf(m *kvpack.Message) []int {
	var keys []int
	for i := range m.Items {
		if m.Items[i].Type == kvpack.Null {
			keys = append(keys, -int(m.Items[i].Key))
		} else {
			keys = append(keys, int(m.Items[i].Key))
		}
	}
	return keys
}

func span(from, to int) []int {
	var keys []int
	for k := from; k <= to; k++ {
		keys = append(keys, k)
	}
	return keys
}

func TestEveryCanonical(t *testing.T) {
	for _, c := range []struct {
		name string
		e    api.Every
		hex  string
	}{{"full", everyFull(), everyFullHex}, {"nulls", everyNulls(), everyNullsHex}, {"zeros", everyZeros(), everyZerosHex}} {
		got := hex.EncodeToString(c.e.Pack().Encode())
		if got != c.hex {
			t.Errorf("%s: encoding differs:\n got %s\nwant %s", c.name, got, c.hex)
			continue
		}
		// what is read writes the same bytes again
		m, err := kvpack.Decode(c.e.Pack().Encode())
		if err != nil {
			t.Fatal(err)
		}
		var back api.Every
		if err := back.Unpack(m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if again := hex.EncodeToString(back.Pack().Encode()); again != c.hex {
			t.Errorf("%s: round trip differs:\n got %s\nwant %s", c.name, again, c.hex)
		}
	}
}

// TestEveryWritten says which keys each message carries: the rule of each
// form, seen on the wire.
func TestEveryWritten(t *testing.T) {
	e := everyFull()
	var want []int
	want = append(want, span(1, 40)...)
	for k := 41; k <= 50; k++ { // two elements per list
		want = append(want, k, k)
	}
	want = append(want, 51, 51, 51, 52) // a definition and two tuples; a definition
	if got := keysOf(e.Pack()); !reflect.DeepEqual(got, want) {
		t.Errorf("full: %v", got)
	}

	// nulls: the required fields, the nullable ones as nulls, and nothing
	// of the optional, the optional nullable nor the lists
	e = everyNulls()
	want = span(1, 10)
	for k := 21; k <= 30; k++ {
		want = append(want, -k)
	}
	want = append(want, 51)
	if got := keysOf(e.Pack()); !reflect.DeepEqual(got, want) {
		t.Errorf("nulls: %v", got)
	}

	// zeros: an optional field at zero is left out; a nullable one and an
	// optional nullable one at zero are values, and travel
	e = everyZeros()
	want = append(append(span(1, 10), span(21, 40)...), 51)
	if got := keysOf(e.Pack()); !reflect.DeepEqual(got, want) {
		t.Errorf("zeros: %v", got)
	}

	// the same message with everything that always travels given by hand,
	// as its empty value: a nil and the empty value are one thing
	e = api.Every{
		RTs: ts(0), RMsg: kvpack.New(0), RRec: &api.Person{},
		NTs: ts(0), NMsg: kvpack.New(0), NRec: &api.Person{},
		OnTs: ts(0), OnMsg: kvpack.New(0), OnRec: &api.Person{},
		RSet: kvpack.NewTupleSet(kvpack.NewTupleDef()),
		OSet: kvpack.NewTupleSet(kvpack.NewTupleDef()), // optional and of no fields: left out
	}
	if got := hex.EncodeToString(e.Pack().Encode()); got != everyZerosHex {
		t.Errorf("zeros by hand:\n got %s\nwant %s", got, everyZerosHex)
	}
}

// TestEveryRead reads the three messages and looks at every field.
func TestEveryRead(t *testing.T) {
	read := func(e api.Every) api.Every {
		t.Helper()
		m, err := kvpack.Decode(e.Pack().Encode())
		if err != nil {
			t.Fatal(err)
		}
		var back api.Every
		if err := back.Unpack(m); err != nil {
			t.Fatal(err)
		}
		return back
	}

	e := read(everyFull())
	if e.RInt32 != -1 || e.RInt64 != 1<<40 || !e.RTs.Equal(ts(1700000000)) || e.RText != "text" ||
		!bytes.Equal(e.RBin, []byte{0, 1, 255}) || !e.RBool || e.RFloat64 != 2.5 || !e.RDs.Equal(day(2026, 11, 12)) ||
		e.RMsg == nil || e.RMsg.Command != 9 || e.RRec == nil || e.RRec.Name != "r" {
		t.Errorf("full, required: %+v", e)
	}
	if e.OInt32 != 11 || e.OInt64 != 12 || !e.OTs.Equal(ts(1700000013)) || e.OText != "o" ||
		!bytes.Equal(e.OBin, []byte{15}) || !e.OBool || e.OFloat64 != 17.5 || !e.ODs.Equal(day(2026, 1, 18)) ||
		e.OMsg == nil || e.OMsg.Command != 19 || e.ORec == nil || e.ORec.Name != "o" {
		t.Errorf("full, optional: %+v", e)
	}
	if e.NInt32 != 21 || e.NInt64 != 22 || !e.NTs.Equal(ts(1700000023)) || e.NText != "n" ||
		!bytes.Equal(e.NBin, []byte{25}) || !e.NBool || e.NFloat64 != 27.5 || !e.NDs.Equal(day(2026, 1, 28)) ||
		e.NMsg == nil || e.NMsg.Command != 29 || e.NRec == nil || e.NRec.Name != "n" {
		t.Errorf("full, nullable: %+v", e)
	}
	if e.NInt32Null || e.NInt64Null || e.NTsNull || e.NTextNull || e.NBinNull ||
		e.NBoolNull || e.NFloat64Null || e.NDsNull || e.NMsgNull || e.NRecNull {
		t.Errorf("full, nullable: a value read as null: %+v", e)
	}
	if e.OnInt32 != 31 || e.OnInt64 != 32 || !e.OnTs.Equal(ts(1700000033)) || e.OnText != "on" ||
		!bytes.Equal(e.OnBin, []byte{35}) || !e.OnBool || e.OnFloat64 != 37.5 || !e.OnDs.Equal(day(2026, 2, 18)) ||
		e.OnMsg == nil || e.OnMsg.Command != 39 || e.OnRec == nil || e.OnRec.Name != "on" {
		t.Errorf("full, optional nullable: %+v", e)
	}
	if e.OnInt32Null || e.OnInt64Null || e.OnTsNull || e.OnTextNull || e.OnBinNull ||
		e.OnBoolNull || e.OnFloat64Null || e.OnDsNull || e.OnMsgNull || e.OnRecNull {
		t.Errorf("full, optional nullable: a value read as null: %+v", e)
	}
	if !reflect.DeepEqual(e.LInt32, []int32{1, -2}) || !reflect.DeepEqual(e.LInt64, []int64{3, -4}) ||
		len(e.LTs) != 2 || !e.LTs[0].Equal(ts(1700000043)) || !e.LTs[1].Equal(ts(0)) ||
		!reflect.DeepEqual(e.LText, []string{"a", ""}) ||
		len(e.LBin) != 2 || !bytes.Equal(e.LBin[0], []byte{1, 2}) || len(e.LBin[1]) != 0 ||
		!reflect.DeepEqual(e.LBool, []bool{true, false}) || !reflect.DeepEqual(e.LFloat64, []float64{0.5, -1.5}) ||
		len(e.LDs) != 2 || !e.LDs[0].Equal(day(2026, 1, 1)) || !e.LDs[1].IsZero() ||
		len(e.LMsg) != 2 || e.LMsg[0].Command != 1 || e.LMsg[1].Command != 2 ||
		!reflect.DeepEqual(e.LRec, []api.Person{{Name: "x"}, {Name: "y"}}) {
		t.Errorf("full, lists: %+v", e)
	}
	if e.RSet == nil || len(e.RSet.Def.Fields) != 9 || len(e.RSet.Rows) != 2 ||
		e.RSet.Def.Fields[3].Name != "s" || string(e.RSet.Def.Fields[3].Meta) != "len=40" {
		t.Fatalf("full, r_set: %+v", e.RSet)
	}
	row := e.RSet.Rows[0]
	if row[0] != int32(1) || row[1] != int64(2) || !row[2].(time.Time).Equal(ts(1700000003)) || row[3] != "Ana" ||
		!bytes.Equal(row[4].([]byte), []byte{4}) || row[5].(*kvpack.Message).Command != 5 ||
		row[6] != true || row[7] != 6.5 || !row[8].(time.Time).Equal(day(2026, 7, 7)) {
		t.Errorf("full, r_set, the tuple with values: %+v", row)
	}
	for i, v := range e.RSet.Rows[1] {
		if v != nil {
			t.Errorf("full, r_set, the null tuple: field %d is %v", i, v)
		}
	}
	if e.OSet == nil || len(e.OSet.Def.Fields) != 1 || len(e.OSet.Rows) != 0 {
		t.Errorf("full, o_set: %+v", e.OSet)
	}

	// nulls: an optional field that did not arrive reads empty; a nullable
	// one that came as a null and an optional nullable one that did not
	// arrive read null
	e = read(everyNulls())
	if e.RInt32 != 0 || e.RInt64 != 0 || !e.RTs.Equal(ts(0)) || e.RText != "" || len(e.RBin) != 0 ||
		e.RBool || e.RFloat64 != 0 || !e.RDs.IsZero() || e.RMsg == nil || e.RRec == nil || e.RRec.Name != "" {
		t.Errorf("nulls, required: %+v", e)
	}
	if e.OInt32 != 0 || e.OInt64 != 0 || !e.OTs.IsZero() || e.OText != "" || len(e.OBin) != 0 ||
		e.OBool || e.OFloat64 != 0 || !e.ODs.IsZero() || e.OMsg != nil || e.ORec != nil {
		t.Errorf("nulls, optional: %+v", e)
	}
	if !e.NInt32Null || !e.NInt64Null || !e.NTsNull || !e.NTextNull || !e.NBinNull ||
		!e.NBoolNull || !e.NFloat64Null || !e.NDsNull || !e.NMsgNull || !e.NRecNull || e.NMsg != nil || e.NRec != nil {
		t.Errorf("nulls, nullable: %+v", e)
	}
	if !e.OnInt32Null || !e.OnInt64Null || !e.OnTsNull || !e.OnTextNull || !e.OnBinNull ||
		!e.OnBoolNull || !e.OnFloat64Null || !e.OnDsNull || !e.OnMsgNull || !e.OnRecNull || e.OnMsg != nil || e.OnRec != nil {
		t.Errorf("nulls, optional nullable: %+v", e)
	}
	if e.LInt32 != nil || e.LInt64 != nil || e.LTs != nil || e.LText != nil || e.LBin != nil ||
		e.LBool != nil || e.LFloat64 != nil || e.LDs != nil || e.LMsg != nil || e.LRec != nil {
		t.Errorf("nulls, lists: %+v", e)
	}
	if e.RSet == nil || len(e.RSet.Rows) != 0 || e.OSet != nil {
		t.Errorf("nulls, tuplesets: %+v %+v", e.RSet, e.OSet)
	}

	// zeros: a zero that arrives is a value, not a null
	e = read(everyZeros())
	if e.NInt32Null || e.NInt64Null || e.NTsNull || e.NTextNull || e.NBinNull ||
		e.NBoolNull || e.NFloat64Null || e.NDsNull || e.NMsgNull || e.NRecNull || e.NMsg == nil || e.NRec == nil {
		t.Errorf("zeros, nullable: %+v", e)
	}
	if e.OnInt32Null || e.OnInt64Null || e.OnTsNull || e.OnTextNull || e.OnBinNull ||
		e.OnBoolNull || e.OnFloat64Null || e.OnDsNull || e.OnMsgNull || e.OnRecNull || e.OnMsg == nil || e.OnRec == nil {
		t.Errorf("zeros, optional nullable: %+v", e)
	}
	if e.NInt32 != 0 || e.NText != "" || e.NBool || !e.NTs.Equal(ts(0)) || !e.NDs.IsZero() ||
		e.OnInt32 != 0 || e.OnText != "" || e.OnBool || !e.OnTs.Equal(ts(0)) || !e.OnDs.IsZero() {
		t.Errorf("zeros, values: %+v", e)
	}
}

// How TestEveryKeyByKey changes one key of the full message.
const (
	dropKey  = iota // the key does not arrive
	nullKey         // it arrives as a null
	otherKey        // it arrives with another type
)

// change returns m with the items of key dropped, or the first of them
// replaced by a null or by a value of another type.
func change(m *kvpack.Message, key uint16, how int) *kvpack.Message {
	out := kvpack.New(m.Command)
	done := false
	for i := range m.Items {
		it := m.Items[i]
		if it.Key != key {
			out.Items = append(out.Items, it)
			continue
		}
		if !done && how == nullKey {
			out.AddNull(key)
		}
		if !done && how == otherKey {
			if it.Type == kvpack.Text {
				out.AddInt32(key, 1)
			} else {
				out.AddText(key, "x")
			}
		}
		done = true
	}
	return out
}

// TestEveryKeyByKey takes the full message and, key by key, drops it, sends
// it as a null and sends it with another type: per form, whether the
// message is still read and what the field then holds, seen by packing
// again what was read (empty or null and optional: not written; null:
// written as a null).
func TestEveryKeyByKey(t *testing.T) {
	const (
		fails  = iota // the message is refused
		empty         // read, and the field is not written again
		isNull        // read, and the field is written again as a null
	)
	// per form, what happens when its key is dropped, null, of another type
	forms := []struct {
		name     string
		from, to uint16
		want     [3]int
	}{
		{"required", 1, 10, [3]int{fails, fails, fails}},
		{"optional", 11, 20, [3]int{empty, empty, empty}},
		{"nullable", 21, 30, [3]int{fails, isNull, fails}},
		{"optional nullable", 31, 40, [3]int{empty, empty, empty}}, // it reads null, and that null is not written
		{"list", 41, 50, [3]int{empty, empty, empty}},
		{"tupleset", 51, 51, [3]int{fails, fails, fails}},
		{"optional tupleset", 52, 52, [3]int{empty, empty, empty}},
	}
	how := []string{"dropped", "null", "of another type"}
	e := everyFull()
	full := e.Pack()
	for _, f := range forms {
		for key := f.from; key <= f.to; key++ {
			for h, want := range f.want {
				var back api.Every
				err := back.Unpack(change(full, key, h))
				name := fmt.Sprintf("%s, key %d %s", f.name, key, how[h])
				if want == fails {
					if err == nil || !strings.Contains(err.Error(), "every: missing ") {
						t.Errorf("%s: read, want it refused (%v)", name, err)
					}
					continue
				}
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				again := back.Pack()
				if want == empty && again.Find(key) != nil {
					t.Errorf("%s: the field holds something: %+v", name, again.Find(key))
				}
				if want == isNull && !again.IsNull(key) {
					t.Errorf("%s: the field is not null: %+v", name, again.Find(key))
				}
				// the null of an optional nullable field is there to see
				if f.name == "optional nullable" && !reflect.ValueOf(back).FieldByName(onNull[key-31]).Bool() {
					t.Errorf("%s: %s is not set", name, onNull[key-31])
				}
			}
		}
	}
}

var onNull = []string{"OnInt32Null", "OnInt64Null", "OnTsNull", "OnTextNull", "OnBinNull",
	"OnBoolNull", "OnFloat64Null", "OnDsNull", "OnMsgNull", "OnRecNull"}

// A record that always holds itself has no empty value: its own would hold
// another, without end. Go and Harbour refuse it (C refuses any record that
// contains itself other than in a list).
func TestHoldsItself(t *testing.T) {
	for src, bad := range map[string]bool{
		"record a = 1\n next a = 1":                                         true,
		"record a = 1\n next a = 1 nullable":                                true,
		"record a = 1\n b b = 1\nrecord b = 2\n a a = 1 optional nullable":  true,
		"record a = 1\n next a = 1 optional":                                false,
		"record a = 1\n next a = 1 list":                                    false,
		"record a = 1\n b b = 1\nrecord b = 2\n a a = 1 optional\n c c = 2": true,
	} {
		if strings.Contains(src, "c c = 2") {
			src += "\nrecord c = 3\n b b = 1"
		}
		f, err := schema.Parse("t.kvp", strings.NewReader(src))
		if err != nil {
			t.Fatal(err)
		}
		_, errGo := gen.Go(f, "t")
		_, _, errHb := gen.Harbour(f, "t", "t.ch")
		for lang, err := range map[string]error{"Go": errGo, "Harbour": errHb} {
			if bad && (err == nil || !strings.Contains(err.Error(), "always holds itself")) {
				t.Errorf("%s, %q: %v, want it refused", lang, src, err)
			}
			if !bad && err != nil {
				t.Errorf("%s, %q: %v", lang, src, err)
			}
		}
	}
}

// The zero Time is no moment: it travels as 0, what C and Harbour write for
// a ts nobody set, in an item and in a tuple.
func TestZeroTime(t *testing.T) {
	set := kvpack.NewTupleSet(kvpack.NewTupleDef().Field("t", kvpack.Time))
	set.Add(time.Time{})
	m, err := kvpack.Decode(kvpack.New(1).AddTime(1, time.Time{}).AddTupleSet(2, set).Encode())
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := m.Time(1); !ok || !v.Equal(ts(0)) {
		t.Errorf("item: %v", v)
	}
	back, ok := m.TupleSet(2)
	if !ok || len(back.Rows) != 1 || !back.Rows[0][0].(time.Time).Equal(ts(0)) {
		t.Errorf("tuple: %+v", back)
	}
}

// A service is the commands of a declaration under a name.
func TestService(t *testing.T) {
	f, err := schema.Parse("t.kvp", strings.NewReader("service sqlbk\ncommand query = 1\n sql text = 1"))
	if err != nil || f.Service != "sqlbk" {
		t.Fatalf("%v %+v", err, f)
	}
	// a field may still be called service, or include
	f, err = schema.Parse("t.kvp", strings.NewReader("command a = 1\n service text = 1\n include int32 = 2"))
	if err != nil || f.Service != "" || len(f.Blocks[0].Fields) != 2 {
		t.Fatalf("%v %+v", err, f)
	}
	for _, c := range []struct{ src, want string }{
		{"service sqlbk", "a service has one or more commands"},
		{"service sqlbk\nrecord r = 1\n x text = 1", "a service has one or more commands"},
		{"service a\nservice b\ncommand c = 1", "already declared (a)"},
		{"service Sql\ncommand c = 1", "lowercase"},
		{"service a = 2\ncommand c = 1", `expected "service name"`},
		{"include \"other.kvp\"\ncommand c = 1", "not read from a file"},
		{"include other.kvp\ncommand c = 1", `expected "include \"file.kvp\""`},
	} {
		_, err := schema.Parse("t.kvp", strings.NewReader(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.src, err, c.want)
		}
	}
}

// write puts the files of a declaration in a directory of their own.
func write(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// include reads other files into the same declaration: one namespace, the
// types resolved once everything is read.
func TestInclude(t *testing.T) {
	dir := write(t, map[string]string{
		// the service: its command uses a record declared in a file it
		// includes, which in turn uses one of a third, and one of this file
		"sqlbk.kvp": "service sqlbk\n" +
			"include \"types/result.kvp\"\n" +
			"include \"types/column.kvp\"\n" + // again, through result.kvp: read once
			"command query = 1\n sql text = 1\n" +
			"reply query\n results result = 1 list\n" +
			"record note = 102\n text text = 1\n",
		"types/result.kvp": "include \"column.kvp\"\n" + // relative to this file
			"record result = 100\n rows tupleset = 1\n columns column = 2 list\n notes note = 3 list\n",
		"types/column.kvp": "record column = 101\n name text = 1\n",
	})
	f, err := schema.ParseFile(filepath.Join(dir, "sqlbk.kvp"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Service != "sqlbk" || len(f.Commands()) != 1 || len(f.Replies()) != 1 || len(f.Records()) != 3 {
		t.Fatalf("%+v", f)
	}
	var result *schema.Block
	for _, b := range f.Records() {
		if b.Name == "result" {
			result = b
		}
	}
	if result == nil || !strings.HasSuffix(filepath.ToSlash(result.File), "types/result.kvp") ||
		result.Fields[1].Record.Name != "column" || result.Fields[2].Record.Name != "note" {
		t.Fatalf("%+v", result)
	}
	// and it generates as one declaration
	if _, err := gen.Go(f, "sqlbk"); err != nil {
		t.Fatal(err)
	}

	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"a name twice in the namespace": {map[string]string{
			"main.kvp":  "include \"other.kvp\"\nrecord result = 1\n x text = 1\n",
			"other.kvp": "record result = 2\n y text = 1\n",
		}, "record result: name already used at"},
		"a number twice": {map[string]string{
			"main.kvp":  "include \"other.kvp\"\nrecord a = 1\n x text = 1\n",
			"other.kvp": "record b = 1\n y text = 1\n",
		}, "number 1 already used by record b"},
		"a service included": {map[string]string{
			"main.kvp":  "service one\ninclude \"other.kvp\"\ncommand a = 1\n",
			"other.kvp": "service two\ncommand b = 2\n",
		}, "a file that declares a service cannot be included"},
		"a file that includes itself": {map[string]string{
			"main.kvp":  "include \"other.kvp\"\ncommand a = 1\n",
			"other.kvp": "include \"main.kvp\"\nrecord b = 2\n x text = 1\n",
		}, "includes itself"},
		"a file that is not there": {map[string]string{
			"main.kvp": "include \"nowhere.kvp\"\ncommand a = 1\n",
		}, "main.kvp:1: include:"},
		"an error in the included file names it": {map[string]string{
			"main.kvp":  "include \"other.kvp\"\ncommand a = 1\n",
			"other.kvp": "record b = 2\n x thing = 1\n",
		}, "other.kvp:2: field x: unknown type"},
	} {
		_, err := schema.ParseFile(filepath.Join(write(t, c.files), "main.kvp"))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// A names section gives the generated types the names the declaration
// wants, per language.
func TestNames(t *testing.T) {
	const src = "service sqlbk\n" +
		"names\n record result = sqlbk_record\n" +
		"names go\n package = sqldb\n command query = sqlbk_query\n reply query = sqlbk_q_response\n" +
		"names c\n prefix = sq\n command * = call_sqlbk_*\n reply * = res_sqlbk_*\n" +
		"names harbour\n command * = call_*\n command ping = just_ping\n" +
		"names go server\n service = sqlbk_service\n" +
		"names * client\n service = sqlbk_client\n" +
		"command query = 1\n sql text = 1\n one result = 2\n" +
		"reply query\n results result = 1 list\n" +
		"command ping = 2\n" +
		"record result = 100\n rows tupleset = 1\n"
	f, err := schema.Parse("t.kvp", strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	block := func(kind schema.Kind, name string) *schema.Block {
		for _, b := range f.Blocks {
			if b.Kind == kind && b.Name == name {
				return b
			}
		}
		t.Fatalf("no %s %s", kind, name)
		return nil
	}
	for _, c := range []struct {
		lang string
		kind schema.Kind
		name string
		want string
	}{
		{schema.Go, schema.Command, "query", "sqlbk_query"},
		{schema.Go, schema.Reply, "query", "sqlbk_q_response"},
		{schema.Go, schema.Record, "result", "sqlbk_record"}, // from the section for all
		{schema.Go, schema.Command, "ping", ""},              // not named: the default
		{schema.C, schema.Command, "query", "call_sqlbk_query"},
		{schema.C, schema.Reply, "query", "res_sqlbk_query"}, // * is the command of the reply
		{schema.C, schema.Command, "ping", "call_sqlbk_ping"},
		{schema.C, schema.Record, "result", "sqlbk_record"},
		{schema.Harbour, schema.Command, "query", "call_query"},
		{schema.Harbour, schema.Command, "ping", "just_ping"}, // its own line wins over the pattern
		{schema.Harbour, schema.Reply, "query", ""},
	} {
		if got := f.TypeName(c.lang, block(c.kind, c.name)); got != c.want {
			t.Errorf("%s, %s %s: %q, want %q", c.lang, c.kind, c.name, got, c.want)
		}
	}
	if f.Package() != "sqldb" || f.Prefix(schema.C) != "sq" || f.Prefix(schema.Harbour) != "" {
		t.Errorf("package %q, prefixes %q %q", f.Package(), f.Prefix(schema.C), f.Prefix(schema.Harbour))
	}
	if f.ServiceName(schema.Go, "server") != "sqlbk_service" || f.ServiceName(schema.Go, "client") != "sqlbk_client" ||
		f.ServiceName(schema.C, "client") != "sqlbk_client" || f.ServiceName(schema.C, "server") != "" {
		t.Error("service names")
	}

	// the generated code carries them, each language in its own convention
	goSrc, err := gen.Go(f, "sqldb")
	if err != nil {
		t.Fatal(err)
	}
	h, c, err := gen.C(f, "sq", "t.h")
	if err != nil {
		t.Fatal(err)
	}
	_, prg, err := gen.Harbour(f, "sqlbk", "t.ch")
	if err != nil {
		t.Fatal(err)
	}
	for what, c := range map[string]struct {
		src  []byte
		want []string
	}{
		"Go": {goSrc, []string{"type SqlbkQuery struct", "type SqlbkQResponse struct", "type SqlbkRecord struct", "type Ping struct",
			"Results []SqlbkRecord", "One *SqlbkRecord", "func (s *SqlbkQuery) Pack()", "CmdQuery ",
			// the service is named in "names go server"
			"type SqlbkService struct", "type SqlbkServiceHandler interface", "func NewSqlbkService(h SqlbkServiceHandler) *SqlbkService",
			"OnQuery(ctx context.Context, msg *QueryMsg)", "Request SqlbkQuery", "Reply   SqlbkQResponse"}},
		"C header": {h, []string{"} call_sqlbk_query;", "} res_sqlbk_query;", "} sqlbk_record;", "} sqlbk_record_st;",
			"sqlbk_record_st one;", "int call_sqlbk_query_encode(", "int res_sqlbk_query_add_results(kvp_buffer *a, res_sqlbk_query *s, const sqlbk_record *v);",
			"#define SQ_CMD_QUERY", "#define SQ_QUERY_SQL",
			// no name for the service in C: the prefix
			"} sq_query_msg;", "call_sqlbk_query request;", "res_sqlbk_query reply;", "} sq_handler;",
			"int sq_dispatch(const sq_handler *h, void *app, const kvp_buffer *b, void *ctx);"}},
		"C source": {c, []string{"sqlbk_record_put(&sub, a, &s->one.value);", "rc = sqlbk_record_read(b, &sub, &s->one.value);"}},
		"Harbour": {prg, []string{"CREATE CLASS CallQuery ", "CREATE CLASS JustPing ", "CREATE CLASS SqlbkQueryReply ", "CREATE CLASS SqlbkRecord ",
			"::one := SqlbkRecord():New()", "oSub := SqlbkRecord():New()",
			"CREATE CLASS SqlbkServer\n", "CREATE CLASS SqlbkQueryMsg INHERIT KvpMsg", "::request := CallQuery():New()", "::handler:OnQuery( oMsg, oCtx )"}},
	} {
		for _, want := range c.want {
			if !bytes.Contains(c.src, []byte(want)) {
				t.Errorf("%s: no %q", what, want)
			}
		}
	}

	for _, c := range []struct{ src, want string }{
		{"names python\ncommand a = 1", "unknown language"},
		{"names go both\ncommand a = 1", "unknown side"},
		{"names go client extra\ncommand a = 1", "expected"},
		{"names\n package = x\ncommand a = 1", "package is of a go section"},
		{"names go\n prefix = x\ncommand a = 1", "prefix is of a c or harbour section"},
		{"names\n command b = x\ncommand a = 1", "names: no command b"},
		{"names\n reply a = x\ncommand a = 1", "names: no reply a"},
		{"names\n record a = x\ncommand a = 1", "names: no record a"},
		{"names\n command a = Upper\ncommand a = 1", "lowercase"},
		{"names\n command * = fixed\ncommand a = 1", "needs one *"},
		{"names\n command a = x\n command a = y\ncommand a = 1", "already named (x)"},
		{"names go\n package = x\n package = y\ncommand a = 1", "already named (x)"},
		{"names c client\n command a = x\ncommand a = 1", "a type is named per language"},
		{"names\n colour = x\ncommand a = 1", "unknown entry"},
		{"names\n service x y\ncommand a = 1", `expected "service = name"`},
	} {
		_, err := schema.Parse("t.kvp", strings.NewReader(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q: %v, want %q", c.src, err, c.want)
		}
	}
	// two types that end up with one name are refused when generating
	f, err = schema.Parse("t.kvp", strings.NewReader("names\n command a = same\n record b = same\ncommand a = 1\nrecord b = 2\n x text = 1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gen.Go(f, "t"); err == nil || !strings.Contains(err.Error(), "are both named same in go") {
		t.Errorf("one name twice: %v", err)
	}
	// a field may still be called names
	if _, err := schema.Parse("t.kvp", strings.NewReader("command a = 1\n names text = 1")); err != nil {
		t.Error(err)
	}
}

// The server side generated from api.kvp, with no transport: a handler of
// the application, the message it gets for each call, and a context the
// multiplexer only passes on. Here the context carries where the handler
// leaves the bytes of what it replies.
type testHandler struct{ calls int }

type replyKey struct{}

func leave(ctx context.Context, b []byte) { *ctx.Value(replyKey{}).(*[]byte) = b }

func (h *testHandler) OnLogin(ctx context.Context, msg *api.LoginMsg) {
	h.calls++
	if msg.Request.Password != "secret" {
		leave(ctx, msg.PackError("wrong password").Encode())
		return
	}
	msg.Reply.Name = strings.ToUpper(msg.Request.User) // the reply is there, empty: just fill it
	msg.Reply.Expires = ts(1700000000)
	leave(ctx, msg.Pack().Encode())
}

func (h *testHandler) OnPing(ctx context.Context, msg *api.PingMsg) {
	h.calls++
	leave(ctx, msg.Pack().Encode())
}

func (h *testHandler) OnQuery(ctx context.Context, msg *api.QueryMsg) {
	h.calls++
	msg.Reply.Code = int32(len(msg.Request.Sql))
	msg.Reply.Rows = kvpack.NewTupleSet(kvpack.NewTupleDef().Field("n", kvpack.Int32))
	msg.Reply.Rows.Add(int32(1))
	leave(ctx, msg.Pack().Encode())
}

func (h *testHandler) OnEvery(ctx context.Context, msg *api.EveryMsg) {
	h.calls++
	leave(ctx, []byte(msg.Request.RText)) // nothing says a handler must reply a message
}

func TestServer(t *testing.T) {
	h := &testHandler{}
	srv := api.NewServer(h)
	call := func(m *kvpack.Message) ([]byte, error) {
		t.Helper()
		in, err := kvpack.Decode(m.Encode()) // as it would arrive
		if err != nil {
			t.Fatal(err)
		}
		var out []byte
		err = srv.Dispatch(context.WithValue(context.Background(), replyKey{}, &out), in)
		return out, err
	}
	decode := func(b []byte) *kvpack.Message {
		t.Helper()
		m, err := kvpack.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	// a call: the request is unpacked into the message, the handler fills the reply
	out, err := call((&api.Login{User: "bob", Password: "secret"}).Pack())
	if err != nil || h.calls != 1 {
		t.Fatalf("%v, %d calls", err, h.calls)
	}
	var login api.LoginReply
	if err := login.Unpack(decode(out)); err != nil || login.Name != "BOB" || !login.Expires.Equal(ts(1700000000)) ||
		login.Events != nil || login.Owner != nil { // what the handler did not touch is empty
		t.Errorf("%v %+v", err, login)
	}
	// an error reply is the handler's to send
	out, err = call((&api.Login{User: "bob", Password: "no"}).Pack())
	if err != nil {
		t.Fatal(err)
	}
	if err := login.Unpack(decode(out)); err == nil || err.Error() != "wrong password" {
		t.Errorf("error reply: %v", err)
	}
	// a command with no reply: an empty message of its number
	out, err = call((&api.Heartbeat{}).Pack())
	if m := decode(out); err != nil || m.Command != api.CmdPing || len(m.Items) != 0 {
		t.Errorf("ping: %v %+v", err, m)
	}
	// a reply with a tupleset
	out, err = call((&api.Query{Sql: "select"}).Pack())
	var query api.QueryReply
	if err != nil {
		t.Fatal(err)
	}
	if err := query.Unpack(decode(out)); err != nil || query.Code != 6 || len(query.Rows.Rows) != 1 || query.Rows.Rows[0][0] != int32(1) {
		t.Errorf("query: %v %+v", err, query)
	}
	// the context is the caller's: the handler may leave anything in it
	every := everyFull()
	every.RText = "as it came"
	if out, err = call(every.Pack()); err != nil || string(out) != "as it came" || h.calls != 5 {
		t.Errorf("every: %v %q, %d calls", err, out, h.calls)
	}

	// a command that is none of these, and a request with a required field
	// missing: nobody is called
	if _, err := call(kvpack.New(99)); err == nil || err.Error() != "unknown command 99" {
		t.Errorf("unknown command: %v", err)
	}
	if _, err := call(kvpack.New(api.CmdLogin).AddText(1, "bob")); err == nil || err.Error() != "login: missing password" {
		t.Errorf("missing field: %v", err)
	}
	if h.calls != 5 {
		t.Errorf("%d calls", h.calls)
	}
}

// loop carries the calls of a client straight to a multiplexer, in memory:
// the two generated sides against each other, with no transport.
type loop struct{ srv *api.Server }

func (l loop) Call(ctx context.Context, req *kvpack.Message) (*kvpack.Message, error) {
	in, err := kvpack.Decode(req.Encode()) // as it would arrive
	if err != nil {
		return nil, err
	}
	var out []byte
	if err := l.srv.Dispatch(context.WithValue(ctx, replyKey{}, &out), in); err != nil {
		return nil, err
	}
	return kvpack.Decode(out)
}

func TestClient(t *testing.T) {
	h := &testHandler{}
	c := api.NewClient(loop{api.NewServer(h)})
	ctx := context.Background()

	// a call: the request out, the reply back, both typed
	reply, err := c.Login(ctx, &api.Login{User: "bob", Password: "secret"})
	if err != nil || reply.Name != "BOB" || !reply.Expires.Equal(ts(1700000000)) {
		t.Fatalf("%v %+v", err, reply)
	}
	// an error reply is the error of the call, with the server's text
	if reply, err := c.Login(ctx, &api.Login{User: "bob", Password: "no"}); err == nil || err.Error() != "wrong password" || reply != nil {
		t.Errorf("error reply: %v %+v", err, reply)
	}
	// a command with no reply gives only an error
	if err := c.Ping(ctx, &api.Heartbeat{}); err != nil { // the request of ping, under the name the declaration gives it
		t.Errorf("ping: %v", err)
	}
	// a reply with a tupleset
	q, err := c.Query(ctx, &api.Query{Sql: "select"})
	if err != nil || q.Code != 6 || len(q.Rows.Rows) != 1 {
		t.Errorf("query: %v %+v", err, q)
	}
	// what the caller could not do is the error of the call: here the
	// handler of every replies bytes that are no message
	every := everyFull()
	if err := c.Every(ctx, &every); err == nil {
		t.Error("every: a reply that is no message was taken")
	}
	if h.calls != 5 {
		t.Errorf("%d calls", h.calls)
	}
}

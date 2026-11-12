package kvpack_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/pablo-botella/kvpack"
)

// canonicalHex is the reference encoding shared with the C and Harbour
// tests (capi/test_kvpack.c, capi/test_hbkvpack.prg): command 7 with
// an int32 -2, an int64 1234567890123, a ts 1700000000, the text "héllo"
// and the bytes 00 ff 10 under keys 1..5.
const canonicalHex = "4b565001070000000500000045000000a12331480100010004000000feffffff0200020008000000cb04fb711f010000030003000800000000f1536500000000040004000600000068c3a96c6c6f050005000300000000ff10"

func canonical() *kvpack.Message {
	return kvpack.New(7).
		AddInt32(1, -2).
		AddInt64(2, 1234567890123).
		AddTime(3, time.Unix(1700000000, 0)).
		AddText(4, "héllo").
		AddBin(5, []byte{0, 0xff, 0x10})
}

func TestEncodeCanonical(t *testing.T) {
	got := hex.EncodeToString(canonical().Encode())
	if got != canonicalHex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonicalHex)
	}
	if canonical().Size() != kvpack.HeaderSize+69 {
		t.Fatalf("Size = %d", canonical().Size())
	}
}

func TestDecodeCanonical(t *testing.T) {
	b, _ := hex.DecodeString(canonicalHex)
	m, err := kvpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Command != 7 || len(m.Items) != 5 {
		t.Fatalf("command %d, %d items", m.Command, len(m.Items))
	}
	if v, ok := m.Int32(1); !ok || v != -2 {
		t.Errorf("int32: %d %v", v, ok)
	}
	if v, ok := m.Int64(2); !ok || v != 1234567890123 {
		t.Errorf("int64: %d %v", v, ok)
	}
	if v, ok := m.Time(3); !ok || v.Unix() != 1700000000 || v.Location() != time.UTC {
		t.Errorf("ts: %v %v", v, ok)
	}
	if v, ok := m.Text(4); !ok || v != "héllo" {
		t.Errorf("text: %q %v", v, ok)
	}
	if v, ok := m.Bin(5); !ok || !bytes.Equal(v, []byte{0, 0xff, 0x10}) {
		t.Errorf("bin: %x %v", v, ok)
	}
	if m.IsError() {
		t.Error("IsError on a plain message")
	}
	// wrong type or absent key
	if _, ok := m.Text(1); ok {
		t.Error("Text on an int32 said ok")
	}
	if _, ok := m.Int32(9); ok {
		t.Error("absent key said ok")
	}
	h, err := kvpack.DecodeHeader(b)
	if err != nil || h.Command != 7 || h.Count != 5 || h.Size != 69 || h.CRC32 != 0x483123a1 {
		t.Errorf("header: %+v %v", h, err)
	}
}

func TestRoundTripAndLists(t *testing.T) {
	m := kvpack.New(1).AddText(10, "a").AddText(10, "b").AddInt32(11, 5)
	got, err := kvpack.Decode(m.Encode())
	if err != nil {
		t.Fatal(err)
	}
	all := got.FindAll(10)
	if len(all) != 2 {
		t.Fatalf("FindAll = %d", len(all))
	}
	if s, _ := all[1].Text(); s != "b" {
		t.Errorf("second = %q", s)
	}
	if s, _ := got.Text(10); s != "a" {
		t.Errorf("first = %q", s)
	}
	if got.Find(12) != nil {
		t.Error("Find of absent key not nil")
	}
	empty, err := kvpack.Decode(kvpack.New(3).Encode())
	if err != nil || empty.Command != 3 || len(empty.Items) != 0 {
		t.Errorf("empty message: %+v %v", empty, err)
	}
}

func TestNestedMessages(t *testing.T) {
	row := func(id int32, name string) *kvpack.Message {
		return kvpack.New(100).AddInt32(1, id).AddText(2, name)
	}
	m := kvpack.New(5).AddMessage(1, row(1, "a")).AddMessage(1, row(2, "b")).AddText(2, "tail")
	got, err := kvpack.Decode(m.Encode())
	if err != nil {
		t.Fatal(err)
	}
	rows := got.FindAll(1)
	if len(rows) != 2 {
		t.Fatalf("rows = %d", len(rows))
	}
	for i, r := range rows {
		sub, ok := r.Message()
		if !ok || sub.Command != 100 {
			t.Fatalf("row %d: %v", i, ok)
		}
		if id, _ := sub.Int32(1); int(id) != i+1 {
			t.Errorf("row %d id %d", i, id)
		}
	}
	if sub, ok := got.Message(1); !ok || sub.Command != 100 {
		t.Error("Message(1) failed")
	}
	if _, ok := got.Message(2); ok {
		t.Error("Message on a text said ok")
	}
	// a corrupt nested message is rejected by the parent's Decode
	bad := kvpack.New(5).Add(1, kvpack.Msg, []byte("KVP\x01junk")).Encode()
	if _, err := kvpack.Decode(bad); !errors.Is(err, kvpack.ErrFormat) {
		t.Errorf("corrupt nested: %v", err)
	}
	if kvpack.Msg.String() != "msg" {
		t.Error("Msg.String")
	}
}

func TestErrorReply(t *testing.T) {
	m, err := kvpack.Decode(kvpack.NewError(9, "no such user").Encode())
	if err != nil {
		t.Fatal(err)
	}
	text, ok := m.ErrorText()
	if !ok || text != "no such user" || !m.IsError() || m.Command != 9 {
		t.Errorf("error reply: %q %v", text, ok)
	}
}

func TestAddPanicsOnBadFixedSize(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("no panic")
		}
	}()
	kvpack.New(1).Add(1, kvpack.Int32, []byte{1, 2})
}

func TestDecodeRejects(t *testing.T) {
	good, _ := hex.DecodeString(canonicalHex)
	flip := func(off int) []byte {
		b := append([]byte(nil), good...)
		b[off] ^= 0xff
		return b
	}
	cases := []struct {
		name string
		b    []byte
		want error
	}{
		{"magic", flip(0), kvpack.ErrMagic},
		{"version", flip(3), kvpack.ErrVersion},
		{"short header", good[:10], kvpack.ErrTruncated},
		{"truncated data", good[:len(good)-1], kvpack.ErrTruncated},
		{"trailing byte", append(append([]byte(nil), good...), 0), kvpack.ErrFormat},
		{"crc", flip(len(good) - 1), kvpack.ErrCRC},
		{"empty", nil, kvpack.ErrTruncated},
	}
	for _, c := range cases {
		_, err := kvpack.Decode(c.b)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.want)
		}
	}
}

// reseal recomputes the crc so the structural checks, not the crc, are
// what rejects a tampered data block.
func reseal(b []byte) []byte {
	m := kvpack.New(0)
	h, _ := kvpack.DecodeHeader(b)
	_ = h
	// rebuild through a Bin item holding the raw data would change the
	// structure; instead patch the crc directly
	data := b[kvpack.HeaderSize:]
	crc := crc32IEEE(data)
	b[16], b[17], b[18], b[19] = byte(crc), byte(crc>>8), byte(crc>>16), byte(crc>>24)
	_ = m
	return b
}

func crc32IEEE(b []byte) uint32 {
	var table [256]uint32
	for i := range table {
		c := uint32(i)
		for k := 0; k < 8; k++ {
			if c&1 != 0 {
				c = 0xedb88320 ^ (c >> 1)
			} else {
				c >>= 1
			}
		}
		table[i] = c
	}
	crc := ^uint32(0)
	for _, x := range b {
		crc = table[byte(crc)^x] ^ (crc >> 8)
	}
	return ^crc
}

func TestDecodeRejectsStructure(t *testing.T) {
	base, _ := hex.DecodeString(canonicalHex)
	tamper := func(f func(b []byte)) []byte {
		b := append([]byte(nil), base...)
		f(b)
		return reseal(b)
	}
	cases := []struct {
		name string
		b    []byte
	}{
		{"nitems too big", tamper(func(b []byte) { b[8] = 6 })},
		{"nitems too small", tamper(func(b []byte) { b[8] = 4 })},
		{"int32 with size 3", tamper(func(b []byte) { b[24] = 3; b[8] = 5 })},
		{"unknown type", tamper(func(b []byte) { b[22] = 99 })},
		{"item size past end", tamper(func(b []byte) { b[len(b)-4-3] = 200 })},
	}
	for _, c := range cases {
		_, err := kvpack.Decode(c.b)
		if !errors.Is(err, kvpack.ErrFormat) {
			t.Errorf("%s: got %v, want ErrFormat", c.name, err)
		}
	}
}

func TestRead(t *testing.T) {
	b := canonical().Encode()
	m, err := kvpack.Read(bytes.NewReader(b), 0)
	if err != nil || m.Command != 7 {
		t.Fatalf("Read: %v", err)
	}
	if _, err := kvpack.Read(bytes.NewReader(b), len(b)-1); !errors.Is(err, kvpack.ErrTooLarge) {
		t.Errorf("limit: %v", err)
	}
	if _, err := kvpack.Read(bytes.NewReader(b), len(b)); err != nil {
		t.Errorf("exact limit: %v", err)
	}
	if _, err := kvpack.Read(bytes.NewReader(b[:30]), 0); !errors.Is(err, kvpack.ErrTruncated) {
		t.Errorf("short stream: %v", err)
	}
	if _, err := kvpack.Read(bytes.NewReader(nil), 0); !errors.Is(err, io.EOF) {
		t.Errorf("empty stream: %v", err)
	}
	// two messages back to back: Read takes exactly one
	r := bytes.NewReader(append(append([]byte(nil), b...), b...))
	if _, err := kvpack.Read(r, 0); err != nil {
		t.Fatal(err)
	}
	if r.Len() != len(b) {
		t.Errorf("Read consumed %d extra bytes", len(b)-r.Len())
	}
	var buf bytes.Buffer
	if n, err := canonical().WriteTo(&buf); err != nil || int(n) != len(b) {
		t.Errorf("WriteTo: %d %v", n, err)
	}
}

// canonical2Hex is the second reference encoding shared with the C and
// Harbour tests: the types added after the first (bool, float64, ds).
// Command 8: bool true (key 1), bool false (2), float64 1.5 (3), ds
// "20261111" (4), null ds, 8 spaces (5).
const canonical2Hex = "4b5650010800000005000000420000009000fc760100070001000000010200070001000000000300080008000000000000000000f83f0400090008000000323032363131313105000900080000002020202020202020"

func canonical2() *kvpack.Message {
	return kvpack.New(8).AddBool(1, true).AddBool(2, false).AddFloat64(3, 1.5).
		AddDate(4, time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)).AddDate(5, time.Time{})
}

func TestCanonical2(t *testing.T) {
	if got := hex.EncodeToString(canonical2().Encode()); got != canonical2Hex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonical2Hex)
	}
	b, _ := hex.DecodeString(canonical2Hex)
	m, err := kvpack.Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := m.Bool(1); !ok || !v {
		t.Error("bool true")
	}
	if v, ok := m.Bool(2); !ok || v {
		t.Error("bool false")
	}
	if v, ok := m.Float64(3); !ok || v != 1.5 {
		t.Errorf("float64 %v", v)
	}
	if v, ok := m.Date(4); !ok || !v.Equal(time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("ds %v", v)
	}
	if v, ok := m.Date(5); !ok || !v.IsZero() {
		t.Errorf("empty ds %v", v)
	}
	if _, ok := m.Bool(3); ok {
		t.Error("a float64 read as bool")
	}
	// the day of the given location, not of UTC
	madrid := time.FixedZone("x", 2*3600)
	d, _ := kvpack.New(1).AddDate(1, time.Date(2026, 1, 1, 0, 30, 0, 0, madrid)).Date(1)
	if d.Day() != 1 || d.Year() != 2026 {
		t.Errorf("AddDate converted the day: %v", d)
	}
}

func TestNewTypesValues(t *testing.T) {
	base, _ := hex.DecodeString(canonical2Hex)
	// value offsets: bool true at 28, the ds 20261111 at 62..69
	b := append([]byte(nil), base...)
	b[28] = 2
	if _, err := kvpack.Decode(reseal(b)); !errors.Is(err, kvpack.ErrFormat) {
		t.Errorf("bool 2: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("raw bool 7: no panic")
			}
		}()
		kvpack.New(1).Add(1, kvpack.Bool, []byte{7})
	}()

	// like SToD, a ds that is not a real day reads as the null date
	for _, s := range []string{"20261311", "20261100", "20260230", "20250229", "00001111", "00000000", "2026111x", "2026    ", "        "} {
		b := append([]byte(nil), base...)
		copy(b[62:70], s) // the value of the ds under key 4
		m, err := kvpack.Decode(reseal(b))
		if err != nil {
			t.Errorf("%q: %v", s, err)
			continue
		}
		if d, ok := m.Date(4); !ok || !d.IsZero() {
			t.Errorf("%q: %v %v, want the null date", s, d, ok)
		}
	}
	b = append([]byte(nil), base...)
	copy(b[62:70], "20240229") // a leap day is a day
	if m, err := kvpack.Decode(reseal(b)); err != nil {
		t.Errorf("29 feb 2024: %v", err)
	} else if d, _ := m.Date(4); d.Day() != 29 {
		t.Errorf("29 feb 2024: %v", d)
	}
	// and what cannot be written as a day is written as the null date
	if v := kvpack.New(1).AddDate(1, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)).Items[0].Value; string(v) != "        " {
		t.Errorf("year 10000: %q", v)
	}
	for _, y := range []int{1, 1850, 9999} {
		if d, _ := kvpack.New(1).AddDate(1, time.Date(y, 12, 31, 0, 0, 0, 0, time.UTC)).Date(1); d.Year() != y {
			t.Errorf("%d: %v", y, d)
		}
	}
}

// canonical3Hex is the third reference encoding shared with the C and
// Harbour tests: the null type. Command 9: int32 7 (key 1), null (2), an
// empty text (3) -- a null is neither a zero nor an empty value.
const canonical3Hex = "4b56500109000000030000001c000000a2ae13ae01000100040000000700000002000a00000000000300040000000000"

func TestCanonical3(t *testing.T) {
	enc := kvpack.New(9).AddInt32(1, 7).AddNull(2).AddText(3, "").Encode()
	if got := hex.EncodeToString(enc); got != canonical3Hex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonical3Hex)
	}
	base, _ := hex.DecodeString(canonical3Hex)
	m, err := kvpack.Decode(base)
	if err != nil {
		t.Fatal(err)
	}
	if !m.IsNull(2) || !m.Find(2).IsNull() || m.Find(2).Type != kvpack.Null {
		t.Error("key 2 is not a null")
	}
	if m.IsNull(1) || m.IsNull(3) {
		t.Error("a value read as null")
	}
	if m.IsNull(9) {
		t.Error("an absent key read as null")
	}
	// the typed getters do not reinterpret a null
	if _, ok := m.Int32(2); ok {
		t.Error("a null read as int32")
	}
	if _, ok := m.Text(2); ok {
		t.Error("a null read as text")
	}
	if v, ok := m.Text(3); !ok || v != "" {
		t.Error("the empty text")
	}
	// a null carries no value: size 1 is refused (the size of key 2 is at 36;
	// the byte it claims is taken from the next item, so nitems drops to 2)
	b := append([]byte(nil), base...)
	b[36] = 8
	b[8] = 2
	if _, err := kvpack.Decode(reseal(b)); !errors.Is(err, kvpack.ErrFormat) {
		t.Errorf("null with a value: %v", err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("raw null with a value: no panic")
			}
		}()
		kvpack.New(1).Add(1, kvpack.Null, []byte{0})
	}()
}

// canonical4Hex is the fourth reference encoding shared with the C and
// Harbour tests: tuples. Command 10. Under key 1 a definition (id int32,
// name text with meta "len=40", born ds) and two tuples: 1, "Ana",
// 1980-01-15 and 2, "Luis", null. Under key 2 a definition (n int64) with
// no tuples.
const canonical4Hex = "4b5650010a000000040000009c000000f158c35d01000b0032000000030000000100020000006964000000000400040000006e616d65060000006c656e3d3430090004000000626f726e0000000001000c002100000001000400000001000000040003000000416e61090008000000313938303031313501000c001a000000010004000000020000000400040000004c7569730a000000000002000b000f000000010000000200010000006e00000000"

func canonical4Def() *kvpack.TupleDef {
	return kvpack.NewTupleDef().Field("id", kvpack.Int32).FieldMeta("name", kvpack.Text, []byte("len=40")).Field("born", kvpack.Date)
}

func TestCanonical4(t *testing.T) {
	born := time.Date(1980, 1, 15, 0, 0, 0, 0, time.UTC)
	ts := kvpack.NewTupleSet(canonical4Def())
	if err := ts.Add(int32(1), "Ana", born); err != nil {
		t.Fatal(err)
	}
	if err := ts.Add(2, "Luis", nil); err != nil { // an int for an int32, nil for null
		t.Fatal(err)
	}
	empty := kvpack.NewTupleSet(kvpack.NewTupleDef().Field("n", kvpack.Int64))
	enc := kvpack.New(10).AddTupleSet(1, ts).AddTupleSet(2, empty).Encode()
	if got := hex.EncodeToString(enc); got != canonical4Hex {
		t.Fatalf("encoding differs:\n got %s\nwant %s", got, canonical4Hex)
	}

	base, _ := hex.DecodeString(canonical4Hex)
	m, err := kvpack.Decode(base)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := m.TupleSet(1)
	if !ok {
		t.Fatal("no tuples under key 1")
	}
	f := got.Def.Fields
	if len(f) != 3 || f[0].Name != "id" || f[0].Type != kvpack.Int32 || len(f[0].Meta) != 0 ||
		f[1].Name != "name" || f[1].Type != kvpack.Text || string(f[1].Meta) != "len=40" ||
		f[2].Name != "born" || f[2].Type != kvpack.Date {
		t.Errorf("definition: %+v", f)
	}
	want := [][]any{{int32(1), "Ana", born}, {int32(2), "Luis", nil}}
	if len(got.Rows) != 2 {
		t.Fatalf("rows: %d", len(got.Rows))
	}
	for i, row := range got.Rows {
		for j, v := range row {
			if v != want[i][j] {
				t.Errorf("row %d field %s: %v, want %v", i, f[j].Name, v, want[i][j])
			}
		}
	}
	if e, ok := m.TupleSet(2); !ok || len(e.Def.Fields) != 1 || len(e.Rows) != 0 {
		t.Error("the definition with no tuples")
	}
	if _, ok := m.TupleSet(9); ok {
		t.Error("tuples under an absent key")
	}
	if d, ok := m.Find(1).TupleDef(); !ok || len(d.Fields) != 3 {
		t.Error("Item.TupleDef")
	}
	if len(m.FindAll(1)) != 3 || m.FindAll(1)[1].Type != kvpack.Tup {
		t.Error("the definition and its two tuples share the key")
	}
}

func TestTupleValues(t *testing.T) {
	// every field type, and a null in each position
	def := kvpack.NewTupleDef().Field("a", kvpack.Int32).Field("b", kvpack.Int64).Field("c", kvpack.Time).
		Field("d", kvpack.Text).Field("e", kvpack.Bin).Field("f", kvpack.Msg).Field("g", kvpack.Bool).
		Field("h", kvpack.Float64).Field("i", kvpack.Date)
	when := time.Unix(1700000000, 0).UTC()
	day := time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC)
	ts := kvpack.NewTupleSet(def)
	if err := ts.Add(int32(-2), int64(1234567890123), when, "", []byte{0, 0xff}, kvpack.New(5).AddInt32(1, 3), true, 1.5, day); err != nil {
		t.Fatal(err)
	}
	if err := ts.Add(nil, nil, nil, nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	m, err := kvpack.Decode(kvpack.New(1).AddTupleSet(7, ts).Encode())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := m.TupleSet(7)
	r := got.Rows[0]
	if r[0] != int32(-2) || r[1] != int64(1234567890123) || r[2] != when || r[3] != "" || r[6] != true || r[7] != 1.5 || r[8] != day {
		t.Errorf("values: %v", r)
	}
	if b, ok := r[4].([]byte); !ok || len(b) != 2 || b[1] != 0xff {
		t.Errorf("bin: %v", r[4])
	}
	if sub, ok := r[5].(*kvpack.Message); !ok || sub.Command != 5 {
		t.Errorf("msg: %v", r[5])
	}
	for i, v := range got.Rows[1] {
		if v != nil {
			t.Errorf("field %d of the null tuple: %v", i, v)
		}
	}

	// values that do not fit the definition are refused, nothing is added
	small := kvpack.NewTupleSet(kvpack.NewTupleDef().Field("n", kvpack.Int32).Field("s", kvpack.Text))
	for name, row := range map[string][]any{
		"too few":       {int32(1)},
		"too many":      {int32(1), "a", "b"},
		"text as int32": {"x", "a"},
		"int32 as text": {int32(1), int32(2)},
		"int too big":   {1 << 40, "a"},
	} {
		if err := small.Add(row...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(small.Rows) != 0 {
		t.Error("a refused tuple was added")
	}

	// a definition a decoder would refuse cannot be built
	for name, f := range map[string]func(){
		"no name":       func() { kvpack.NewTupleDef().Field("", kvpack.Int32) },
		"repeated name": func() { kvpack.NewTupleDef().Field("a", kvpack.Int32).Field("a", kvpack.Text) },
		"null field":    func() { kvpack.NewTupleDef().Field("a", kvpack.Null) },
		"tuple field":   func() { kvpack.NewTupleDef().Field("a", kvpack.Tup) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: no panic", name)
				}
			}()
			f()
		}()
	}
}

func TestTupleOrder(t *testing.T) {
	def := kvpack.NewTupleDef().Field("n", kvpack.Int32).Field("s", kvpack.Text)
	row, err := def.EncodeRow(int32(1), "a")
	if err != nil {
		t.Fatal(err)
	}
	d := def.Encode()
	raw := func(key uint16, ty kvpack.Type, v []byte) func(*kvpack.Message) {
		return func(m *kvpack.Message) { m.Add(key, ty, v) }
	}
	defOf := func(f func(d *kvpack.TupleDef)) []byte { // a definition no builder lets through
		x := &kvpack.TupleDef{Fields: []kvpack.TupleField{{Name: "n", Type: kvpack.Int32}, {Name: "s", Type: kvpack.Text}}}
		f(x)
		return x.Encode()
	}
	good := [][]func(*kvpack.Message){
		{raw(1, kvpack.TupDef, d)},
		{raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, row), raw(1, kvpack.Tup, row)},
		{raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, row), raw(2, kvpack.TupDef, d), raw(2, kvpack.Tup, row)},
		{raw(1, kvpack.TupDef, d), raw(5, kvpack.Int32, []byte{1, 0, 0, 0})},
	}
	bad := map[string][]func(*kvpack.Message){
		"tuple with no definition":        {raw(1, kvpack.Tup, row)},
		"tuple under another key":         {raw(1, kvpack.TupDef, d), raw(2, kvpack.Tup, row)},
		"an item in between":              {raw(1, kvpack.TupDef, d), raw(5, kvpack.Int32, []byte{1, 0, 0, 0}), raw(1, kvpack.Tup, row)},
		"tuple after another definition":  {raw(1, kvpack.TupDef, d), raw(2, kvpack.TupDef, d), raw(1, kvpack.Tup, row)},
		"two definitions under one key":   {raw(1, kvpack.TupDef, d), raw(1, kvpack.TupDef, d)},
		"a second one further on":         {raw(1, kvpack.TupDef, d), raw(2, kvpack.TupDef, d), raw(1, kvpack.TupDef, d)},
		"tuple one field short":           {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, row[:10])},
		"tuple with bytes left over":      {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, append(append([]byte(nil), row...), 0))},
		"int32 field of 3 bytes":          {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, []byte{1, 0, 3, 0, 0, 0, 1, 0, 0, 4, 0, 0, 0, 0, 0})},
		"field size past the end":         {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, []byte{1, 0, 4, 0, 0, 0, 1, 0, 0, 0, 4, 0, 9, 0, 0, 0, 'a'})},
		"field of another type":           {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, []byte{4, 0, 4, 0, 0, 0, 1, 0, 0, 0, 4, 0, 1, 0, 0, 0, 'a'})},
		"null with a value":               {raw(1, kvpack.TupDef, d), raw(1, kvpack.Tup, []byte{10, 0, 1, 0, 0, 0, 0, 4, 0, 1, 0, 0, 0, 'a'})},
		"definition cut short":            {raw(1, kvpack.TupDef, d[:len(d)-1])},
		"definition with bytes left over": {raw(1, kvpack.TupDef, append(append([]byte(nil), d...), 0))},
		"definition shorter than a count": {raw(1, kvpack.TupDef, []byte{0, 0})},
		"field with no name":              {raw(1, kvpack.TupDef, defOf(func(x *kvpack.TupleDef) { x.Fields[1].Name = "" }))},
		"repeated field name":             {raw(1, kvpack.TupDef, defOf(func(x *kvpack.TupleDef) { x.Fields[1].Name = "n" }))},
		"null as a field type":            {raw(1, kvpack.TupDef, defOf(func(x *kvpack.TupleDef) { x.Fields[1].Type = kvpack.Null }))},
		"tuple as a field type":           {raw(1, kvpack.TupDef, defOf(func(x *kvpack.TupleDef) { x.Fields[1].Type = kvpack.Tup }))},
	}
	build := func(steps []func(*kvpack.Message)) []byte {
		m := kvpack.New(1)
		for _, s := range steps {
			s(m)
		}
		return m.Encode()
	}
	for i, steps := range good {
		if _, err := kvpack.Decode(build(steps)); err != nil {
			t.Errorf("good %d: %v", i, err)
		}
	}
	for name, steps := range bad {
		if _, err := kvpack.Decode(build(steps)); !errors.Is(err, kvpack.ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// a bool field is still 0 or 1, and a nested message must decode
	bdef := kvpack.NewTupleDef().Field("b", kvpack.Bool).Field("m", kvpack.Msg)
	for name, row := range map[string][]byte{
		"bool 2":      {7, 0, 1, 0, 0, 0, 2, 10, 0, 0, 0, 0, 0},
		"msg of junk": {10, 0, 0, 0, 0, 0, 6, 0, 4, 0, 0, 0, 'j', 'u', 'n', 'k'},
	} {
		b := kvpack.New(1).Add(1, kvpack.TupDef, bdef.Encode()).Add(1, kvpack.Tup, row).Encode()
		if _, err := kvpack.Decode(b); !errors.Is(err, kvpack.ErrFormat) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestTypeString(t *testing.T) {
	want := map[kvpack.Type]string{kvpack.Int32: "int32", kvpack.Int64: "int64", kvpack.Time: "ts", kvpack.Text: "text", kvpack.Bin: "bin",
		kvpack.Msg: "msg", kvpack.Bool: "bool", kvpack.Float64: "float64", kvpack.Date: "ds", kvpack.Null: "null", kvpack.TupDef: "tupledef", kvpack.Tup: "tuple", kvpack.Type(99): "type(99)"}
	for ty, s := range want {
		if ty.String() != s {
			t.Errorf("%d: %q", ty, ty.String())
		}
	}
}

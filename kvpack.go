// Package kvpack is a tiny binary format that packs the data of a call (a
// request or its reply) between programs written in Go, C and Harbour: a
// command number plus a flat list of typed key/value items, with a crc32
// over the payload. No transport, no dependencies. What the keys mean is
// the application's contract; the format only carries them, and the
// kvpack command (cmd/kvpack) can generate named code for them from a .kvp
// declaration.
//
// A message is built with [New] and the Add* methods, turned into bytes with
// [Message.Encode] (or [Message.WriteTo]), and read back with [Decode] (bytes
// already in hand) or [Read] (from a stream, bounded). Carrying the bytes
// (an HTTP body, a socket, a file) is the application's business.
//
// # Format
//
//	HEADER (20 bytes, little-endian):
//	  0   magic     "KVP" (3)
//	  3   version   uint8   (1)
//	  4   command   uint32  application command number
//	  8   nitems    uint32  number of items in DATA
//	  12  size      uint32  byte length of DATA
//	  16  crc32     uint32  crc32 (IEEE) of DATA
//	DATA (size bytes): nitems items, back to back:
//	  key       uint16  application key; 0 is reserved for an error text
//	  type      uint16  Int32 1, Int64 2, Time 3, Text 4, Bin 5, Msg 6,
//	                    Bool 7, Float64 8, Date 9
//	  size      uint32  byte length of value
//	  value     size bytes
//
// Value encodings: Int32 is 4 bytes, Int64 and Time 8 bytes (Time is Unix
// seconds, UTC, signed), Text is UTF-8 without a terminator, Bin is raw,
// Msg is a whole message (header included) nested as a value: a record,
// whose command number says which kind. Bool is 1 byte, 0 or 1. Float64 is
// 8 bytes IEEE 754. Date (ds) is 8 ASCII digits yyyymmdd, a calendar day
// as Harbour's DToS writes it; 8 spaces (DToS of an empty date) are the
// null date, and like SToD a reader takes anything that is not a real day
// ("00000000", "20260230") as the null date. A key may repeat (a list);
// readers take the first unless they iterate. A reply carrying key 0 (a Text) is an
// error reply: see [NewError] and [Message.ErrorText].
package kvpack

// The documentation is not automatic — this makes it: `go generate ./...`
// rebuilds every artifact from _mkskill/ (README, AGENTS.md and the skill;
// the tool version is pinned by go.mod), and the golden test fails when
// anything went stale. The second line propagates the version-spec's
// destinations (__publish.bat) without touching the version.
//go:generate go run github.com/pablo-botella/mkskill/cmd/mkskill -q build
//go:generate go run github.com/pablo-botella/mkskill/cmd/mkskill -q -vbuild

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"time"
)

const (
	// Version is the format version written in the header.
	Version = 1
	// HeaderSize is the fixed size of the message header.
	HeaderSize = 20
	// ItemSize is the fixed size of an item header (key, type, size).
	ItemSize = 8
	// KeyError is the reserved key of an error reply (a Text item).
	KeyError uint16 = 0
)

var magic = [3]byte{'K', 'V', 'P'}

// Type is the type tag of an item value.
type Type uint16

// Item value types.
const (
	Int32   Type = 1 // 4 bytes, little-endian, signed
	Int64   Type = 2 // 8 bytes, little-endian, signed
	Time    Type = 3 // 8 bytes: Unix seconds, UTC, signed
	Text    Type = 4 // UTF-8, no terminator
	Bin     Type = 5 // raw bytes
	Msg     Type = 6 // a nested message, header included
	Bool    Type = 7 // 1 byte: 0 or 1
	Float64 Type = 8 // 8 bytes, IEEE 754, little-endian
	Date    Type = 9 // 8 ASCII digits yyyymmdd (DToS); 8 spaces = null date, as anything malformed
)

// nullDate is the value of the null Date, as DToS writes an empty date.
const nullDate = "        "

// String names the type as the manifest and the tools do.
func (t Type) String() string {
	switch t {
	case Int32:
		return "int32"
	case Int64:
		return "int64"
	case Time:
		return "ts"
	case Text:
		return "text"
	case Bin:
		return "bin"
	case Msg:
		return "msg"
	case Bool:
		return "bool"
	case Float64:
		return "float64"
	case Date:
		return "ds"
	}
	return fmt.Sprintf("type(%d)", uint16(t))
}

// fixedSize is the value size a type demands; 0 when any size is valid.
func (t Type) fixedSize() int {
	switch t {
	case Int32:
		return 4
	case Int64, Time, Float64, Date:
		return 8
	case Bool:
		return 1
	}
	return 0
}

// valid reports whether t is a known type.
func (t Type) valid() bool {
	return t >= Int32 && t <= Date
}

// validValue checks what a fixed size alone does not: a Bool is 0 or 1. A
// Date is never refused: like Harbour's SToD, whatever is not a real day
// reads as the null date.
func (t Type) validValue(v []byte) bool {
	if t == Bool {
		return v[0] <= 1
	}
	return true
}

// parseDate reads a Date value as SToD does: midnight UTC of the day for 8
// digits yyyymmdd of a real day (years 1..9999), the zero time.Time (the
// null date) for anything else — 8 spaces, "00000000", a 30th of February.
func parseDate(v []byte) time.Time {
	n := 0
	for _, c := range v {
		if c < '0' || c > '9' {
			return time.Time{}
		}
		n = n*10 + int(c-'0')
	}
	y, m, d := n/10000, time.Month(n/100%100), n%100
	if y < 1 || m < 1 || m > 12 || d < 1 {
		return time.Time{}
	}
	t := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return time.Time{}
	}
	return t
}

// Errors returned by the decoders.
var (
	ErrMagic     = errors.New("kvpack: not a message")
	ErrVersion   = errors.New("kvpack: unsupported version")
	ErrTruncated = errors.New("kvpack: message cut short")
	ErrFormat    = errors.New("kvpack: inconsistent message")
	ErrCRC       = errors.New("kvpack: crc32 mismatch")
	ErrTooLarge  = errors.New("kvpack: message larger than allowed")
)

// Item is one key/value of a message.
type Item struct {
	Key   uint16
	Type  Type
	Value []byte
}

// Int32 returns the value when the item is an Int32.
func (it *Item) Int32() (int32, bool) {
	if it == nil || it.Type != Int32 || len(it.Value) != 4 {
		return 0, false
	}
	return int32(binary.LittleEndian.Uint32(it.Value)), true
}

// Int64 returns the value when the item is an Int64.
func (it *Item) Int64() (int64, bool) {
	if it == nil || it.Type != Int64 || len(it.Value) != 8 {
		return 0, false
	}
	return int64(binary.LittleEndian.Uint64(it.Value)), true
}

// Time returns the value when the item is a Time (Unix seconds, UTC).
func (it *Item) Time() (time.Time, bool) {
	if it == nil || it.Type != Time || len(it.Value) != 8 {
		return time.Time{}, false
	}
	return time.Unix(int64(binary.LittleEndian.Uint64(it.Value)), 0).UTC(), true
}

// Text returns the value when the item is a Text.
func (it *Item) Text() (string, bool) {
	if it == nil || it.Type != Text {
		return "", false
	}
	return string(it.Value), true
}

// Bin returns the value when the item is a Bin. The bytes are the item's
// own: copy them to keep them past the message.
func (it *Item) Bin() ([]byte, bool) {
	if it == nil || it.Type != Bin {
		return nil, false
	}
	return it.Value, true
}

// Message returns the value decoded as a message when the item is a Msg.
// Its values alias the parent's bytes.
func (it *Item) Message() (*Message, bool) {
	if it == nil || it.Type != Msg {
		return nil, false
	}
	m, err := Decode(it.Value)
	return m, err == nil
}

// Bool returns the value when the item is a Bool.
func (it *Item) Bool() (bool, bool) {
	if it == nil || it.Type != Bool || len(it.Value) != 1 {
		return false, false
	}
	return it.Value[0] == 1, true
}

// Float64 returns the value when the item is a Float64.
func (it *Item) Float64() (float64, bool) {
	if it == nil || it.Type != Float64 || len(it.Value) != 8 {
		return 0, false
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(it.Value)), true
}

// Date returns the value when the item is a Date: midnight UTC of that
// day, or the zero time.Time for the null date (or a malformed one).
func (it *Item) Date() (time.Time, bool) {
	if it == nil || it.Type != Date || len(it.Value) != 8 {
		return time.Time{}, false
	}
	return parseDate(it.Value), true
}

// Message is a command with its items. Build one with New and the Add*
// methods; Decode and Read return one.
type Message struct {
	Command uint32
	Items   []Item
}

// New starts a message for command.
func New(command uint32) *Message {
	return &Message{Command: command}
}

// NewError builds an error reply to command: a single Text item under
// KeyError. A caller reads it with ErrorText.
func NewError(command uint32, text string) *Message {
	return New(command).AddText(KeyError, text)
}

// Add appends an item as given. The value is kept by reference. It panics
// when the size a type demands is not met, or the value is one a decoder
// would refuse (a Bool other than 0/1), which is a programming error.
func (m *Message) Add(key uint16, t Type, value []byte) *Message {
	if n := t.fixedSize(); n != 0 && len(value) != n {
		panic(fmt.Sprintf("kvpack: %s value must be %d bytes, got %d", t, n, len(value)))
	}
	if !t.validValue(value) {
		panic(fmt.Sprintf("kvpack: invalid %s value % x", t, value))
	}
	m.Items = append(m.Items, Item{Key: key, Type: t, Value: value})
	return m
}

// AddInt32 appends an Int32 item.
func (m *Message) AddInt32(key uint16, v int32) *Message {
	return m.Add(key, Int32, binary.LittleEndian.AppendUint32(nil, uint32(v)))
}

// AddInt64 appends an Int64 item.
func (m *Message) AddInt64(key uint16, v int64) *Message {
	return m.Add(key, Int64, binary.LittleEndian.AppendUint64(nil, uint64(v)))
}

// AddTime appends a Time item: t as Unix seconds (any location; the wire
// value is UTC by definition).
func (m *Message) AddTime(key uint16, t time.Time) *Message {
	return m.Add(key, Time, binary.LittleEndian.AppendUint64(nil, uint64(t.Unix())))
}

// AddText appends a Text item.
func (m *Message) AddText(key uint16, s string) *Message {
	return m.Add(key, Text, []byte(s))
}

// AddBin appends a Bin item. The bytes are kept by reference.
func (m *Message) AddBin(key uint16, b []byte) *Message {
	return m.Add(key, Bin, b)
}

// AddMessage appends a Msg item: sub encoded in place. A nested message
// is a record; its command number tells which kind.
func (m *Message) AddMessage(key uint16, sub *Message) *Message {
	return m.Add(key, Msg, sub.Encode())
}

// AddBool appends a Bool item.
func (m *Message) AddBool(key uint16, v bool) *Message {
	b := []byte{0}
	if v {
		b[0] = 1
	}
	return m.Add(key, Bool, b)
}

// AddFloat64 appends a Float64 item.
func (m *Message) AddFloat64(key uint16, v float64) *Message {
	return m.Add(key, Float64, binary.LittleEndian.AppendUint64(nil, math.Float64bits(v)))
}

// AddDate appends a Date item: the calendar day of t in its own location
// (no conversion to UTC) as yyyymmdd, or the null date (8 spaces) when t is
// the zero time.Time or its year is outside 1..9999.
func (m *Message) AddDate(key uint16, t time.Time) *Message {
	y, mo, d := t.Date()
	if t.IsZero() || y < 1 || y > 9999 {
		return m.Add(key, Date, []byte(nullDate))
	}
	return m.Add(key, Date, fmt.Appendf(nil, "%04d%02d%02d", y, mo, d))
}

// Find returns the first item with key, or nil.
func (m *Message) Find(key uint16) *Item {
	for i := range m.Items {
		if m.Items[i].Key == key {
			return &m.Items[i]
		}
	}
	return nil
}

// FindAll returns every item with key, in order (a repeated key is a list).
func (m *Message) FindAll(key uint16) []*Item {
	var out []*Item
	for i := range m.Items {
		if m.Items[i].Key == key {
			out = append(out, &m.Items[i])
		}
	}
	return out
}

// Int32 returns the first item with key as an Int32; ok is false when the
// key is absent or holds another type.
func (m *Message) Int32(key uint16) (v int32, ok bool) { return m.Find(key).Int32() }

// Int64 returns the first item with key as an Int64.
func (m *Message) Int64(key uint16) (v int64, ok bool) { return m.Find(key).Int64() }

// Time returns the first item with key as a Time.
func (m *Message) Time(key uint16) (v time.Time, ok bool) { return m.Find(key).Time() }

// Text returns the first item with key as a Text.
func (m *Message) Text(key uint16) (v string, ok bool) { return m.Find(key).Text() }

// Bin returns the first item with key as a Bin.
func (m *Message) Bin(key uint16) (v []byte, ok bool) { return m.Find(key).Bin() }

// Message returns the first item with key as a nested message.
func (m *Message) Message(key uint16) (v *Message, ok bool) { return m.Find(key).Message() }

// Bool returns the first item with key as a Bool.
func (m *Message) Bool(key uint16) (v bool, ok bool) { return m.Find(key).Bool() }

// Float64 returns the first item with key as a Float64.
func (m *Message) Float64(key uint16) (v float64, ok bool) { return m.Find(key).Float64() }

// Date returns the first item with key as a Date (midnight UTC; zero when empty).
func (m *Message) Date(key uint16) (v time.Time, ok bool) { return m.Find(key).Date() }

// ErrorText returns the text of an error reply (a Text item under
// KeyError); ok is false when the message is not an error.
func (m *Message) ErrorText() (text string, ok bool) { return m.Text(KeyError) }

// IsError reports whether the message carries an error text.
func (m *Message) IsError() bool { _, ok := m.ErrorText(); return ok }

// DataSize is the byte length of the data block.
func (m *Message) DataSize() int {
	n := 0
	for i := range m.Items {
		n += ItemSize + len(m.Items[i].Value)
	}
	return n
}

// Size is the byte length of the encoded message, header included.
func (m *Message) Size() int {
	return HeaderSize + m.DataSize()
}

// Encode returns the message as bytes.
func (m *Message) Encode() []byte {
	b := make([]byte, m.Size())
	copy(b[0:3], magic[:])
	b[3] = Version
	binary.LittleEndian.PutUint32(b[4:], m.Command)
	binary.LittleEndian.PutUint32(b[8:], uint32(len(m.Items)))
	binary.LittleEndian.PutUint32(b[12:], uint32(m.DataSize()))
	p := HeaderSize
	for i := range m.Items {
		it := &m.Items[i]
		binary.LittleEndian.PutUint16(b[p:], it.Key)
		binary.LittleEndian.PutUint16(b[p+2:], uint16(it.Type))
		binary.LittleEndian.PutUint32(b[p+4:], uint32(len(it.Value)))
		p += ItemSize
		p += copy(b[p:], it.Value)
	}
	binary.LittleEndian.PutUint32(b[16:], crc32.ChecksumIEEE(b[HeaderSize:]))
	return b
}

// WriteTo writes the encoded message to w.
func (m *Message) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(m.Encode())
	return int64(n), err
}

// Header is the fixed part of a message, as read by DecodeHeader.
type Header struct {
	Command uint32
	Count   uint32 // number of items
	Size    uint32 // byte length of the data block
	CRC32   uint32 // crc32 of the data block
}

// DecodeHeader parses the first HeaderSize bytes of a message: magic and
// version are checked, nothing else. It is the way to learn how many more
// bytes a stream holds (Header.Size) before reading them.
func DecodeHeader(b []byte) (Header, error) {
	if len(b) < HeaderSize {
		return Header{}, ErrTruncated
	}
	if b[0] != magic[0] || b[1] != magic[1] || b[2] != magic[2] {
		return Header{}, ErrMagic
	}
	if b[3] != Version {
		return Header{}, ErrVersion
	}
	return Header{
		Command: binary.LittleEndian.Uint32(b[4:]),
		Count:   binary.LittleEndian.Uint32(b[8:]),
		Size:    binary.LittleEndian.Uint32(b[12:]),
		CRC32:   binary.LittleEndian.Uint32(b[16:]),
	}, nil
}

// Decode parses a whole message held in b: header, every item (walked and
// checked against nitems, size and the fixed sizes of the types) and the
// crc32. Item values alias b; copy what must outlive it. Trailing bytes
// after the message are an error.
func Decode(b []byte) (*Message, error) {
	h, err := DecodeHeader(b)
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) < HeaderSize+uint64(h.Size) {
		return nil, ErrTruncated
	}
	if uint64(len(b)) != HeaderSize+uint64(h.Size) {
		return nil, ErrFormat
	}
	data := b[HeaderSize:]
	if crc32.ChecksumIEEE(data) != h.CRC32 {
		return nil, ErrCRC
	}
	return decodeData(h, data)
}

// decodeData walks the items of a data block already verified by crc.
func decodeData(h Header, data []byte) (*Message, error) {
	if uint64(h.Count)*ItemSize > uint64(len(data)) {
		return nil, ErrFormat
	}
	m := &Message{Command: h.Command, Items: make([]Item, 0, h.Count)}
	p := 0
	for i := uint32(0); i < h.Count; i++ {
		if len(data)-p < ItemSize {
			return nil, ErrFormat
		}
		it := Item{
			Key:  binary.LittleEndian.Uint16(data[p:]),
			Type: Type(binary.LittleEndian.Uint16(data[p+2:])),
		}
		size := binary.LittleEndian.Uint32(data[p+4:])
		p += ItemSize
		if uint64(size) > uint64(len(data)-p) {
			return nil, ErrFormat
		}
		if !it.Type.valid() {
			return nil, ErrFormat
		}
		if n := it.Type.fixedSize(); n != 0 && int(size) != n {
			return nil, ErrFormat
		}
		it.Value = data[p : p+int(size)]
		if !it.Type.validValue(it.Value) {
			return nil, ErrFormat
		}
		p += int(size)
		if it.Type == Msg {
			if _, err := Decode(it.Value); err != nil {
				return nil, fmt.Errorf("%w (nested message under key %d: %v)", ErrFormat, it.Key, err)
			}
		}
		m.Items = append(m.Items, it)
	}
	if p != len(data) {
		return nil, ErrFormat
	}
	return m, nil
}

// Read reads one message from r: the header first, then exactly the data
// it announces, refusing with ErrTooLarge any message whose total size
// exceeds max (max <= 0 means no limit). The result owns its bytes. A
// stream that ends before the first byte returns io.EOF (no message at
// all); one that ends inside a message returns ErrTruncated.
func Read(r io.Reader, max int) (*Message, error) {
	var hb [HeaderSize]byte
	if _, err := io.ReadFull(r, hb[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
	h, err := DecodeHeader(hb[:])
	if err != nil {
		return nil, err
	}
	if max > 0 && HeaderSize+uint64(h.Size) > uint64(max) {
		return nil, ErrTooLarge
	}
	b := make([]byte, HeaderSize+int(h.Size))
	copy(b, hb[:])
	if _, err := io.ReadFull(r, b[HeaderSize:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return nil, ErrTruncated
		}
		return nil, err
	}
	return Decode(b)
}

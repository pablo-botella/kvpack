---
mkskill:
  pos: 50
  in: readme
---

## Go API

### Building

```go
func New(command uint32) *Message
func NewError(command uint32, text string) *Message   // a Text under KeyError (0)

func (m *Message) AddInt32(key uint16, v int32) *Message
func (m *Message) AddInt64(key uint16, v int64) *Message
func (m *Message) AddTime(key uint16, t time.Time) *Message   // stored as Unix seconds, UTC
func (m *Message) AddText(key uint16, s string) *Message
func (m *Message) AddBin(key uint16, b []byte) *Message       // kept by reference
func (m *Message) AddMessage(key uint16, sub *Message) *Message
func (m *Message) AddBool(key uint16, v bool) *Message
func (m *Message) AddFloat64(key uint16, v float64) *Message
func (m *Message) AddDate(key uint16, t time.Time) *Message   // the day of t in its own location; zero time = null date
func (m *Message) Add(key uint16, t Type, value []byte) *Message   // raw; panics on a value a decoder would refuse
```

The Add* methods return the message, so a reply reads as one expression.
Items keep the order they were added in.

### Encoding

```go
func (m *Message) Encode() []byte
func (m *Message) WriteTo(w io.Writer) (int64, error)
func (m *Message) Size() int        // encoded length, header included
func (m *Message) DataSize() int
```

### Decoding

```go
func Decode(b []byte) (*Message, error)            // b is exactly one message; values alias b
func Read(r io.Reader, max int) (*Message, error)  // one message from a stream, bounded; owns its bytes
func DecodeHeader(b []byte) (Header, error)        // the first 20 bytes: how much follows

type Header struct {
    Command uint32
    Count   uint32   // items
    Size    uint32   // bytes of data after the header
    CRC32   uint32
}
```

`Decode` checks magic, version, that `len(b)` is exactly header + size,
the crc32, then walks every item: count, each size inside the data,
known type, the fixed sizes, a `Bool` of 0 or 1, and recursively any
nested `Msg`. A `Date` is never refused: like Harbour's `SToD()`, what is
not a real day reads as the null date (the zero `time.Time`). It returns `ErrMagic`, `ErrVersion`, `ErrTruncated`,
`ErrFormat` or `ErrCRC`. `Read` adds `ErrTooLarge` when header + size
exceeds `max` (`max <= 0`: no limit); a stream that ends before the first
byte gives `io.EOF`, one that ends inside a message `ErrTruncated`.

### Reading

```go
func (m *Message) Int32(key uint16) (int32, bool)
func (m *Message) Int64(key uint16) (int64, bool)
func (m *Message) Time(key uint16) (time.Time, bool)      // UTC
func (m *Message) Text(key uint16) (string, bool)
func (m *Message) Bin(key uint16) ([]byte, bool)          // aliases the message
func (m *Message) Message(key uint16) (*Message, bool)    // a nested message
func (m *Message) Bool(key uint16) (bool, bool)
func (m *Message) Float64(key uint16) (float64, bool)
func (m *Message) Date(key uint16) (time.Time, bool)      // midnight UTC; zero time for the null date
func (m *Message) ErrorText() (string, bool)              // key 0 as text
func (m *Message) IsError() bool

func (m *Message) Find(key uint16) *Item       // first item with key, or nil
func (m *Message) FindAll(key uint16) []*Item  // every item with key, in order
```

The typed getters take the **first** item with that key and say `false`
when the key is absent **or holds another type**; a wrong type is never
silently reinterpreted. `Item` has the same typed methods (`it.Int32()`,
`it.Text()`, `it.Message()`...) for walking `m.Items` or `FindAll`.

```go
type Item struct {
    Key   uint16
    Type  Type
    Value []byte
}

type Type uint16
const (
    Int32 Type = 1
    Int64 Type = 2
    Time  Type = 3
    Text  Type = 4
    Bin   Type = 5
    Msg   Type = 6
    Bool    Type = 7
    Float64 Type = 8
    Date    Type = 9   // "ds" in the .kvp language
)
```

`Type.String()` gives `int32`, `int64`, `ts`, `text`, `bin`, `msg`,
`bool`, `float64`, `ds`.

---


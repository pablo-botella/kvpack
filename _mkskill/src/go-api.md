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
func (m *Message) AddTime(key uint16, t time.Time) *Message   // stored as Unix seconds, UTC; the zero Time as 0
func (m *Message) AddText(key uint16, s string) *Message
func (m *Message) AddBin(key uint16, b []byte) *Message       // kept by reference
func (m *Message) AddMessage(key uint16, sub *Message) *Message
func (m *Message) AddBool(key uint16, v bool) *Message
func (m *Message) AddFloat64(key uint16, v float64) *Message
func (m *Message) AddDate(key uint16, t time.Time) *Message   // the day of t in its own location; zero time = null date
func (m *Message) AddNull(key uint16) *Message                // present, no value
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
known type, the fixed sizes, a `Bool` of 0 or 1, recursively any
nested `Msg`, and every tuple against its definition and its place. A `Date` is never refused: like Harbour's `SToD()`, what is
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
func (m *Message) IsNull(key uint16) bool                 // a Null under key; false when absent
func (m *Message) ErrorText() (string, bool)              // key 0 as text
func (m *Message) IsError() bool

func (m *Message) Find(key uint16) *Item       // first item with key, or nil
func (m *Message) FindAll(key uint16) []*Item  // every item with key, in order
```

The typed getters take the **first** item with that key and say `false`
when the key is absent **or holds another type**; a wrong type is never
silently reinterpreted. `Item` has the same typed methods (`it.Int32()`,
`it.Text()`, `it.Message()`...) for walking `m.Items` or `FindAll`.
A `Null` is such another type: every typed getter says `false` on it, and
`IsNull` (on the message or on an item) is what tells it from an absent key.

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
    Null    Type = 10  // no value: size 0
    TupDef  Type = 11  // the definition of the tuples that follow it
    Tup     Type = 12  // a tuple: the values of the fields of its definition
)
```

`Type.String()` gives `int32`, `int64`, `ts`, `text`, `bin`, `msg`,
`bool`, `float64`, `ds`, `null`, `tupledef`, `tuple`.

`it.Any()` gives the value as the Go type of the item's type (`int32`,
`int64`, `time.Time`, `string`, `[]byte`, `*Message`, `bool`, `float64`,
`*TupleDef`, `nil` for a `Null`), for walking what has no shape known in
advance.

### Tuples

For data whose shape is only known when the message is built — the rows of
a query — a structure is declared on the fly and then repeated:

```go
type TupleField struct {
    Name string
    Type Type     // Int32 .. Date
    Meta []byte   // the application's: carried as it is, never looked at
}
type TupleDef struct{ Fields []TupleField }
type TupleSet struct {
    Def  *TupleDef
    Rows [][]any  // one value per field; nil is a null
}

func NewTupleDef() *TupleDef
func (d *TupleDef) Field(name string, t Type) *TupleDef
func (d *TupleDef) FieldMeta(name string, t Type, meta []byte) *TupleDef

func NewTupleSet(def *TupleDef) *TupleSet
func (ts *TupleSet) Add(values ...any) error   // refused, and not added, when it does not fit the definition

func (m *Message) AddTupleSet(key uint16, ts *TupleSet) *Message   // the definition and one item per tuple
func (m *Message) TupleSet(key uint16) (*TupleSet, bool)
```

Neither side writes the fields in its code: the sender builds the
definition from what it has at hand, the receiver asks the definition what
came.

```go
def := kvpack.NewTupleDef()
for _, col := range columns {
    def.FieldMeta(col.Name, typeOf(col), col.Extra)
}
rows := kvpack.NewTupleSet(def)
for result.Next() {
    if err := rows.Add(result.Values()...); err != nil { ... }
}
m.AddTupleSet(21, rows)

// the other side
ts, _ := m.TupleSet(21)
for _, row := range ts.Rows {
    for i, v := range row {              // v is nil for a null
        fmt.Println(ts.Def.Fields[i].Name, "=", v)
    }
}
```

A row value is of the Go type of its field's type: `int32`, `int64`,
`time.Time` (`Time` and `Date`), `string`, `[]byte`, `*Message`, `bool`,
`float64`. `Add` also takes an `int` for an `Int32` (when it fits) or an
`Int64`. `Decode` has already checked every tuple against its definition
and the order of the items, so `TupleSet` on a decoded message cannot
fail for a malformed tuple. `TupleDef.EncodeRow`, `TupleDef.DecodeRow`,
`DecodeTupleDef` and `Item.TupleDef` work one item at a time.

---


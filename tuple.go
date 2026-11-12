package kvpack

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// Tuples are for data whose shape is only known when the message is built
// (the rows of a query, say): a structure declared on the fly and then
// repeated. Under one key travel a TupDef item, the definition, and right
// after it zero or more Tup items, one per tuple, each holding only values
// in the order of the definition.
//
// On the wire:
//
//	TupDef value:  nfields uint32, then per field
//	               type uint16 | name size uint32 | name | meta size uint32 | meta
//	Tup value:     per field of the definition, in order
//	               type uint16 | size uint32 | value
//
// The name is UTF-8. The meta is the application's: raw bytes carried as
// they are and never looked at. A field takes any value type (Int32 to
// Date); a tuple holds no tuples. In a tuple each value says its type
// again: the one the definition declares for the field, or Null (size 0)
// when the field is null.
//
// The order is strict and a decoder enforces it: a key has at most one
// TupDef, a Tup comes right after its TupDef or after another Tup of the
// same key, and every tuple carries exactly the fields of the definition,
// each of its type and the size that type demands, or null.

// TupleField is one field of a tuple definition.
type TupleField struct {
	Name string
	Type Type
	Meta []byte // the application's, opaque; may be empty
}

// TupleDef is the definition of the tuples that follow it under its key.
type TupleDef struct {
	Fields []TupleField
}

// TupleSet is a definition with its tuples, as [Message.AddTupleSet] writes
// them and [Message.TupleSet] reads them. A row holds one value per field,
// of the Go type of the field's type: int32, int64, time.Time (Time and
// Date), string, []byte, *Message, bool, float64, or nil for a null.
type TupleSet struct {
	Def  *TupleDef
	Rows [][]any
}

// tupleField reports whether a tuple field may be of type t: the value
// types, not Null (any field can be null) nor tuples themselves.
func (t Type) tupleField() bool {
	return t >= Int32 && t <= Date
}

// NewTupleDef starts an empty definition.
func NewTupleDef() *TupleDef {
	return &TupleDef{}
}

// Field appends a field with no meta. It panics on a definition a decoder
// would refuse: an empty or repeated name, a type a field cannot have.
func (d *TupleDef) Field(name string, t Type) *TupleDef {
	return d.FieldMeta(name, t, nil)
}

// FieldMeta appends a field with its meta, kept by reference.
func (d *TupleDef) FieldMeta(name string, t Type, meta []byte) *TupleDef {
	if err := d.checkField(name, t); err != nil {
		panic("kvpack: " + err.Error())
	}
	d.Fields = append(d.Fields, TupleField{Name: name, Type: t, Meta: meta})
	return d
}

func (d *TupleDef) checkField(name string, t Type) error {
	if name == "" {
		return fmt.Errorf("tuple field %d has no name", len(d.Fields)+1)
	}
	if !t.tupleField() {
		return fmt.Errorf("tuple field %s cannot be of type %s", name, t)
	}
	for i := range d.Fields {
		if d.Fields[i].Name == name {
			return fmt.Errorf("tuple field %s is repeated", name)
		}
	}
	return nil
}

// Encode returns the definition as the bytes of a TupDef value.
func (d *TupleDef) Encode() []byte {
	n := 4
	for i := range d.Fields {
		n += 2 + 4 + len(d.Fields[i].Name) + 4 + len(d.Fields[i].Meta)
	}
	b := make([]byte, 4, n)
	binary.LittleEndian.PutUint32(b, uint32(len(d.Fields)))
	for i := range d.Fields {
		f := &d.Fields[i]
		b = binary.LittleEndian.AppendUint16(b, uint16(f.Type))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(f.Name)))
		b = append(b, f.Name...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(f.Meta)))
		b = append(b, f.Meta...)
	}
	return b
}

// DecodeTupleDef parses the bytes of a TupDef value, checking what Decode
// checks: the field count, every size, the types, and that names are
// neither empty nor repeated. Metas alias b.
func DecodeTupleDef(b []byte) (*TupleDef, error) {
	if len(b) < 4 {
		return nil, ErrFormat
	}
	count := binary.LittleEndian.Uint32(b)
	p := 4
	if uint64(count)*10 > uint64(len(b)-p) { // a field takes at least 10 bytes
		return nil, ErrFormat
	}
	d := &TupleDef{Fields: make([]TupleField, 0, count)}
	chunk := func() ([]byte, bool) {
		if len(b)-p < 4 {
			return nil, false
		}
		size := binary.LittleEndian.Uint32(b[p:])
		p += 4
		if uint64(size) > uint64(len(b)-p) {
			return nil, false
		}
		v := b[p : p+int(size)]
		p += int(size)
		return v, true
	}
	for i := uint32(0); i < count; i++ {
		if len(b)-p < 2 {
			return nil, ErrFormat
		}
		t := Type(binary.LittleEndian.Uint16(b[p:]))
		p += 2
		name, ok := chunk()
		if !ok {
			return nil, ErrFormat
		}
		meta, ok := chunk()
		if !ok || d.checkField(string(name), t) != nil {
			return nil, ErrFormat
		}
		if len(meta) == 0 {
			meta = nil // as a definition built with Field has it
		}
		d.Fields = append(d.Fields, TupleField{Name: string(name), Type: t, Meta: meta})
	}
	if p != len(b) {
		return nil, ErrFormat
	}
	return d, nil
}

// EncodeRow returns one tuple as the bytes of a Tup value: values holds
// one value per field, in order, of the Go type of the field's type (an int
// is also taken for Int32, when it fits, and for Int64), or nil for null.
func (d *TupleDef) EncodeRow(values ...any) ([]byte, error) {
	if len(values) != len(d.Fields) {
		return nil, fmt.Errorf("kvpack: tuple of %d values for %d fields", len(values), len(d.Fields))
	}
	var b []byte
	for i, v := range values {
		f := &d.Fields[i]
		if v == nil {
			b = binary.LittleEndian.AppendUint16(b, uint16(Null))
			b = binary.LittleEndian.AppendUint32(b, 0)
			continue
		}
		val, ok := fieldValue(f.Type, v)
		if !ok {
			return nil, fmt.Errorf("kvpack: tuple field %s (%s) cannot take a %T", f.Name, f.Type, v)
		}
		b = binary.LittleEndian.AppendUint16(b, uint16(f.Type))
		b = binary.LittleEndian.AppendUint32(b, uint32(len(val)))
		b = append(b, val...)
	}
	return b, nil
}

// fieldValue encodes v as a value of type t; ok is false when v is not of
// a Go type that t takes.
func fieldValue(t Type, v any) (val []byte, ok bool) {
	switch t {
	case Int32:
		switch x := v.(type) {
		case int32:
			return int32Value(x), true
		case int:
			if x >= math.MinInt32 && x <= math.MaxInt32 {
				return int32Value(int32(x)), true
			}
		}
	case Int64:
		switch x := v.(type) {
		case int64:
			return int64Value(x), true
		case int:
			return int64Value(int64(x)), true
		case int32:
			return int64Value(int64(x)), true
		}
	case Time:
		if x, ok := v.(time.Time); ok {
			return timeValue(x), true
		}
	case Text:
		if x, ok := v.(string); ok {
			return []byte(x), true
		}
	case Bin:
		if x, ok := v.([]byte); ok {
			return x, true
		}
	case Msg:
		if x, ok := v.(*Message); ok && x != nil {
			return x.Encode(), true
		}
	case Bool:
		if x, ok := v.(bool); ok {
			return boolValue(x), true
		}
	case Float64:
		if x, ok := v.(float64); ok {
			return float64Value(x), true
		}
	case Date:
		if x, ok := v.(time.Time); ok {
			return dateValue(x), true
		}
	}
	return nil, false
}

// DecodeRow parses the bytes of a Tup value against the definition and
// returns its values, nil for a null field. Bin values alias b.
func (d *TupleDef) DecodeRow(b []byte) ([]any, error) {
	row := make([]any, len(d.Fields))
	p := 0
	for i := range d.Fields {
		if len(b)-p < 6 {
			return nil, ErrFormat
		}
		t := Type(binary.LittleEndian.Uint16(b[p:]))
		size := binary.LittleEndian.Uint32(b[p+2:])
		p += 6
		if t == Null {
			if size != 0 {
				return nil, ErrFormat
			}
			continue
		}
		if t != d.Fields[i].Type || uint64(size) > uint64(len(b)-p) {
			return nil, ErrFormat
		}
		it := Item{Type: d.Fields[i].Type, Value: b[p : p+int(size) : p+int(size)]}
		p += int(size)
		if n := it.Type.fixedSize(); n >= 0 && int(size) != n {
			return nil, ErrFormat
		}
		if !it.Type.validValue(it.Value) {
			return nil, ErrFormat
		}
		if row[i] = it.Any(); row[i] == nil { // a nested message that does not decode
			return nil, ErrFormat
		}
	}
	if p != len(b) {
		return nil, ErrFormat
	}
	return row, nil
}

// NewTupleSet starts a set of tuples of the given definition.
func NewTupleSet(def *TupleDef) *TupleSet {
	return &TupleSet{Def: def}
}

// Add appends one tuple: one value per field of the definition, in order,
// as [TupleDef.EncodeRow] takes them. It fails, adding nothing, when the
// values do not fit the definition.
func (ts *TupleSet) Add(values ...any) error {
	if _, err := ts.Def.EncodeRow(values...); err != nil {
		return err
	}
	ts.Rows = append(ts.Rows, values)
	return nil
}

// AddTupleSet appends the definition (a TupDef item) and then one Tup item
// per row, all under key. It panics when a row does not fit the
// definition, which [TupleSet.Add] never lets in.
func (m *Message) AddTupleSet(key uint16, ts *TupleSet) *Message {
	m.Add(key, TupDef, ts.Def.Encode())
	for _, row := range ts.Rows {
		b, err := ts.Def.EncodeRow(row...)
		if err != nil {
			panic(err.Error())
		}
		m.Add(key, Tup, b)
	}
	return m
}

// TupleSet returns the definition under key with the tuples that follow it;
// ok is false when key holds no TupDef (or, in a message not built by
// Decode, when what follows does not fit it).
func (m *Message) TupleSet(key uint16) (ts *TupleSet, ok bool) {
	for i := range m.Items {
		if m.Items[i].Key != key || m.Items[i].Type != TupDef {
			continue
		}
		def, ok := m.Items[i].TupleDef()
		if !ok {
			return nil, false
		}
		ts = &TupleSet{Def: def}
		for _, it := range m.Items[i+1:] {
			if it.Key != key || it.Type != Tup {
				break
			}
			row, err := def.DecodeRow(it.Value)
			if err != nil {
				return nil, false
			}
			ts.Rows = append(ts.Rows, row)
		}
		return ts, true
	}
	return nil, false
}

// TupleDef returns the value decoded as a tuple definition when the item
// is a TupDef.
func (it *Item) TupleDef() (*TupleDef, bool) {
	if it == nil || it.Type != TupDef {
		return nil, false
	}
	d, err := DecodeTupleDef(it.Value)
	return d, err == nil
}

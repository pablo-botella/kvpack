---
mkskill:
  pos: 210
  in: ai*
---

## Package API

### Build and encode

```go
m := kvpack.New(CmdLogin).AddText(KUser, "bob").AddTime(KWhen, time.Now())
b := m.Encode()                      // []byte, header included; m.Size() is len(b)
n, err := m.WriteTo(conn)            // one message onto a stream
e := kvpack.NewError(CmdLogin, "wrong password")   // a Text under KeyError
reply.AddMessage(KEvent, kvpack.New(RecEvent).AddInt32(KID, 7))   // a record; repeat the key for a list
```

`Add(key, Type, []byte)` is the raw form; it panics when an `Int32`/`Int64`/
`Time` value is not 4/8 bytes (programming error). `AddBin` keeps the slice
by reference.

### Decode and read

```go
m, err := kvpack.Decode(b)            // b is exactly one message; values alias b
m, err := kvpack.Read(r, max)         // one message from a stream, at most max bytes (<=0: no limit)
h, err := kvpack.DecodeHeader(b[:20]) // Command, Count, Size, CRC32 — how much follows

v, ok := m.Int32(key)    // first item with key, as int32; ok=false if absent OR another type
v, ok := m.Int64(key)
t, ok := m.Time(key)     // time.Time in UTC
s, ok := m.Text(key)
p, ok := m.Bin(key)      // aliases the message
sub, ok := m.Message(key)
text, isErr := m.ErrorText();  m.IsError()

it := m.Find(key)           // *Item or nil;  it.Int32(), it.Text(), it.Message()...
for _, it := range m.FindAll(key) { ... }   // a repeated key, in order
for _, it := range m.Items { it.Key, it.Type, it.Value }
```

Errors: `ErrMagic`, `ErrVersion`, `ErrTruncated`, `ErrFormat` (count,
sizes, unknown type, bad fixed size, trailing bytes, bad nested message),
`ErrCRC`, `ErrTooLarge` (`Read` over `max`). `Read` on an already-ended
stream returns `io.EOF`.

### Transport

The package has none. Over HTTP the message is the body:

```go
req, err := kvpack.Read(r.Body, 1<<20)                    // server: bounded read
w.Header().Set("Content-Type", "application/x-kvpack"); reply.WriteTo(w)
resp, err := http.Post(url, "application/x-kvpack", bytes.NewReader(req.Encode()))   // client
reply, err := kvpack.Read(resp.Body, 1<<20)
if text, bad := reply.ErrorText(); bad { ... }            // the server's error, status was 200
```

### C and Harbour (capi/)

```c
kvp_begin(&w, buf, cap, cmd); kvp_add_text(&w, k, "x"); kvp_add_ts(&w, k2, t); kvp_end(&w, &len);
kvp_parse(bytes, n, &m); kvp_get_int32(&m, k, &v); kvp_get_text(&m, k, dst, cap); kvp_get_msg(&m, k, &sub);
kvp_find(&m, k, &it); kvp_find_next(&m, &it);   /* lists */
```

```xbase
hMsg := KvpNew( nCmd ); KvpAddText( hMsg, nKey, cText ); cBytes := KvpEncode( hMsg )
hIn := KvpParse( cBytes, @nErr ); KvpGet( hIn, nKey ); KvpGetAll( hIn, nKey ); KvpErrorText( hIn )
```

Details and the full lists in `capi/README.md`.

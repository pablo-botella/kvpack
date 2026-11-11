---
mkskill:
  pos: 60
  in: readme
---

## Carrying a message

kvpack has no transport: a message is a run of bytes and goes wherever bytes
go. `Encode`/`WriteTo` produce them; `Decode` takes bytes already in hand and
`Read(r, max)` takes exactly one message from a stream, leaving the next one
in it. The 20-byte header (`DecodeHeader`) is all a reader needs to know how
much to read.

As the body of an HTTP `POST`, for instance, it is a few lines on each side:

```go
// server
req, err := kvpack.Read(r.Body, 1<<20)
if err != nil {
    http.Error(w, err.Error(), http.StatusBadRequest)
    return
}
w.Header().Set("Content-Type", "application/x-kvpack")
reply.WriteTo(w)

// client
resp, err := http.Post(url, "application/x-kvpack", bytes.NewReader(req.Encode()))
// check err and resp.StatusCode, then:
reply, err := kvpack.Read(resp.Body, 1<<20)
```

Keep an error reply (`NewError`) on status `200`: the message is the
protocol, the status reports only the transport. Always pass a `max` to
`Read` on anything that comes from a network.

---

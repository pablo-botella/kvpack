---
mkskill:
  pos: 40
  in: readme
---

## Quick start

Declare the commands and keys once per side (they are plain numbers):

```go
const (
    CmdLogin uint32 = 1
    KUser    uint16 = 1
    KPass    uint16 = 2
    KWhen    uint16 = 3
    KName    uint16 = 1
    KExpires uint16 = 2
)
```

### Server (Go)

```go
func serve(req *kvpack.Message) *kvpack.Message {
    switch req.Command {
    case CmdLogin:
        user, _ := req.Text(KUser)
        pass, _ := req.Text(KPass)
        u, err := authenticate(user, pass)
        if err != nil {
            return kvpack.NewError(CmdLogin, "wrong user or password")
        }
        return kvpack.New(CmdLogin).AddText(KName, u.Name).AddTime(KExpires, u.Expires)
    }
    return kvpack.NewError(req.Command, "unknown command")
}
```

`req` comes from `kvpack.Read(body, 1<<20)` (bounded to 1 MB here); the
reply goes back with `WriteTo` — see [Carrying a message](#carrying-a-message).

### Client (Go)

```go
req := kvpack.New(CmdLogin).AddText(KUser, "bob").AddText(KPass, "secret").AddTime(KWhen, time.Now())
reply, err := send(req)                          // your transport: Encode out, Read back
if err != nil {
    return err                                   // transport or decoding
}
if text, bad := reply.ErrorText(); bad {
    return errors.New(text)                      // the server said no
}
name, _ := reply.Text(KName)
expires, _ := reply.Time(KExpires)
```

The same client in C or Harbour is in [`capi/README.md`](capi/README.md):
same bytes, same keys.

### Records and lists

A value can be a whole message (`Msg`), and a key can repeat:

```go
reply := kvpack.New(CmdEvents)
for _, e := range events {
    reply.AddMessage(KEvent, kvpack.New(RecEvent).AddInt32(KID, e.ID).AddText(KTitle, e.Title))
}
// reading
for _, it := range reply.FindAll(KEvent) {
    ev, _ := it.Message()
    id, _ := ev.Int32(KID)
}
```

---


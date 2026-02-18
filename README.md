# bilibili_dm_lib

Go library for subscribing to Bilibili live room danmaku (弹幕) streams via WebSocket.

## Features

- **Pub/Sub API** — typed callbacks (`OnDanmaku`, `OnGift`, etc.) and channel-based subscription
- **Multiple rooms** — subscribe to many rooms with a single client
- **Auto-reconnect** — exponential backoff on disconnect
- **Brotli + Zlib** — handles all Bilibili compression formats
- **Thread-safe** — register handlers from any goroutine
- **Cookie support** — optional authenticated access for richer data
- **Clean shutdown** — context cancellation propagates everywhere

## Install

```bash
go get github.com/MatchaCake/bilibili_dm_lib
```

## Quick Start

### Callback-based

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"

    dm "github.com/MatchaCake/bilibili_dm_lib"
)

func main() {
    client := dm.NewClient(
        dm.WithRoomID(510), // short room IDs are resolved automatically
    )

    client.OnDanmaku(func(d *dm.Danmaku) {
        fmt.Printf("[弹幕] %s: %s\n", d.Sender, d.Content)
    })

    client.OnGift(func(g *dm.Gift) {
        fmt.Printf("[礼物] %s %s %s x%d\n", g.User, g.Action, g.GiftName, g.Num)
    })

    client.OnSuperChat(func(sc *dm.SuperChat) {
        fmt.Printf("[SC ¥%d] %s: %s\n", sc.Price, sc.User, sc.Message)
    })

    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    client.Start(ctx)
}
```

### Channel-based

```go
client := dm.NewClient(dm.WithRoomID(21452505))
events := client.Subscribe()

go client.Start(ctx)

for ev := range events {
    switch d := ev.Data.(type) {
    case *dm.Danmaku:
        fmt.Printf("%s: %s\n", d.Sender, d.Content)
    case *dm.Gift:
        fmt.Printf("Gift: %s x%d from %s\n", d.GiftName, d.Num, d.User)
    }
}
```

### Multiple Rooms

```go
client := dm.NewClient(
    dm.WithRoomID(510),
    dm.WithRoomID(21452505),
)

// Or add rooms dynamically after Start:
client.AddRoom(12345)
client.RemoveRoom(510)
```

### Authenticated (with cookies)

Providing cookies enables richer danmaku data (full medal info, etc.):

```go
client := dm.NewClient(
    dm.WithRoomID(510),
    dm.WithCookie("your_SESSDATA", "your_bili_jct"),
)
```

## Event Types

| CMD | Callback | Struct | Description |
|-----|----------|--------|-------------|
| `DANMU_MSG` | `OnDanmaku` | `Danmaku` | Chat messages |
| `SEND_GIFT` | `OnGift` | `Gift` | Gift events |
| `SUPER_CHAT_MESSAGE` | `OnSuperChat` | `SuperChat` | Super Chat messages |
| `GUARD_BUY` | `OnGuardBuy` | `GuardBuy` | Captain/Admiral/Governor purchases |
| `LIVE` | `OnLive` | `LiveEvent` | Room goes live |
| `PREPARING` | `OnPreparing` | `LiveEvent` | Room goes offline |
| `INTERACT_WORD` | `OnInteractWord` | `InteractWord` | Entry, follow, share |
| *(any)* | `OnRawEvent` | `[]byte` | Catch-all for unrecognised commands |

## Running the Example

```bash
go run ./cmd/example -room 510
```

With cookies:
```bash
go run ./cmd/example -room 510 -sessdata YOUR_SESSDATA -bili-jct YOUR_BILI_JCT
```

## Architecture

```
Client (pub/sub hub)
├── roomConn (room 510)     ← goroutine: connect → auth → read loop
│   ├── heartbeat goroutine ← sends heartbeat every 30s
│   └── auto-reconnect      ← exponential backoff on failure
├── roomConn (room 21452505)
│   └── ...
└── dispatch
    ├── typed callbacks (OnDanmaku, OnGift, ...)
    ├── raw event callback (OnRawEvent)
    └── channel subscribers (Subscribe)
```

## Protocol

This library implements the Bilibili live WebSocket danmaku protocol:

1. Resolve short room ID → real room ID via HTTP API
2. Fetch WSS server host + auth token via HTTP API
3. Connect to `wss://{host}:{port}/sub`
4. Send auth packet (16-byte header + JSON body, protover=3)
5. Send heartbeat every 30 seconds
6. Receive command packets (raw JSON, Brotli, or Zlib compressed)

Packets use a 16-byte big-endian header:
- `[0:4]` Total size, `[4:6]` Header size (16), `[6:8]` Protocol version, `[8:12]` Operation type, `[12:16]` Sequence

## License

MIT

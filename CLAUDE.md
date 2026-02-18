# CLAUDE.md — bilibili_dm_lib

## Overview
Go library for Bilibili live room danmaku (弹幕) — both receiving (WebSocket) and sending (HTTP API).
Pub/sub pattern: callbacks + channel-based subscription.

## Module
`github.com/MatchaCake/bilibili_dm_lib`

## Architecture
- `client.go` — Main Client, multi-room management, event dispatch, subscriber channels
- `conn.go` — Per-room WebSocket connection, heartbeat, auto-reconnect with exponential backoff
- `packet.go` — Binary protocol encode/decode (16-byte header, Brotli/Zlib decompression)
- `events.go` — Event type definitions and CMD parsing (DANMU_MSG, SEND_GIFT, SUPER_CHAT_MESSAGE, etc.)
- `api.go` — HTTP API calls (room_init, getDanmuInfo for WS server/token)
- `options.go` — Client options (WithCookie, WithRoomID, sender-related)
- `sender.go` — Standalone Sender for sending danmaku via HTTP POST
- `sender_options.go` — Sender options (WithSenderCookie, WithMaxLength, WithCooldown)

## Key Design Decisions
- One `roomConn` goroutine per room, decoupled from pub/sub layer
- `sync.RWMutex` for handler registration (readers dispatch, writers register)
- `sync.Map` for per-room rate limiting in Sender
- Rune-based message splitting (not byte-based) for CJK correctness
- Cookie required for sending; optional for receiving (but recommended for full danmaku info)

## WebSocket Protocol
- Connect: `wss://{host}:{wss_port}/sub`
- Auth packet: Protocol=Special(1), Type=Certificate(7), JSON body with roomid+key+protover=3(Brotli)
- Heartbeat: every 30s, Protocol=Special(1), Type=Heartbeat(2)
- Commands: Type=Command(5), may be Brotli/Zlib compressed with nested packets

## Dependencies
- `github.com/gorilla/websocket` — WebSocket
- `github.com/andybalholm/brotli` — Brotli decompression
- `log/slog` — Logging (no external logger)

## Build & Test
```bash
go build ./...
go vet ./...
```

## Git
- Author: MatchaCake <MatchaCake@users.noreply.github.com>
- No Co-Authored-By lines

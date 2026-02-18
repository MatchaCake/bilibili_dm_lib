package dm

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	heartbeatInterval = 30 * time.Second
	maxBackoff        = 2 * time.Minute
	baseBackoff       = 1 * time.Second
)

// roomConn manages a single WebSocket connection to a Bilibili live room.
type roomConn struct {
	shortRoomID int64
	realRoomID  int64
	httpClient  *http.Client
	cookies     string
	dispatch    func(roomID int64, pkt *Packet) // callback into client for event dispatch
	logger      *slog.Logger
}

// run connects to the room and reads messages until the context is cancelled.
// It automatically reconnects on failure with exponential backoff.
func (rc *roomConn) run(ctx context.Context) {
	var attempt int
	for {
		err := rc.connect(ctx)
		if ctx.Err() != nil {
			return // context cancelled — clean shutdown
		}

		attempt++
		delay := backoff(attempt)
		rc.logger.Warn("disconnected, reconnecting",
			"room", rc.shortRoomID,
			"error", err,
			"attempt", attempt,
			"backoff", delay,
		)

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// connect performs a single connection lifecycle: resolve → connect → auth → read loop.
func (rc *roomConn) connect(ctx context.Context) error {
	// Resolve real room ID if not already known.
	if rc.realRoomID == 0 {
		info, err := getRoomInfo(ctx, rc.httpClient, rc.shortRoomID, rc.cookies)
		if err != nil {
			return fmt.Errorf("resolve room: %w", err)
		}
		rc.realRoomID = info.RealRoomID
		rc.logger.Info("resolved room ID", "short", rc.shortRoomID, "real", rc.realRoomID)
	}

	// Get danmu connection info.
	dInfo, err := getDanmuInfo(ctx, rc.httpClient, rc.realRoomID, rc.cookies)
	if err != nil {
		return fmt.Errorf("get danmu info: %w", err)
	}

	// Connect WebSocket.
	wssURL := fmt.Sprintf("wss://%s:%d/sub", dInfo.Host, dInfo.Port)
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	header := http.Header{}
	header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	if rc.cookies != "" {
		header.Set("Cookie", rc.cookies)
	}

	ws, _, err := dialer.DialContext(ctx, wssURL, header)
	if err != nil {
		return fmt.Errorf("websocket dial: %w", err)
	}
	defer ws.Close()

	rc.logger.Info("connected", "room", rc.shortRoomID, "url", wssURL)

	// Send auth packet.
	authPkt := buildAuthPacket(rc.realRoomID, dInfo.Token)
	if err := ws.WriteMessage(websocket.BinaryMessage, authPkt); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// Start heartbeat goroutine.
	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	go rc.heartbeatLoop(hbCtx, ws)

	// Read loop.
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		_, message, err := ws.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		packets, err := decodePackets(message)
		if err != nil {
			rc.logger.Warn("decode error", "room", rc.shortRoomID, "error", err)
			continue
		}

		for _, pkt := range packets {
			rc.dispatch(rc.realRoomID, pkt)
		}
	}
}

// heartbeatLoop sends heartbeat packets at regular intervals.
func (rc *roomConn) heartbeatLoop(ctx context.Context, ws *websocket.Conn) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hb := buildHeartbeatPacket()
			if err := ws.WriteMessage(websocket.BinaryMessage, hb); err != nil {
				rc.logger.Warn("heartbeat send failed", "room", rc.shortRoomID, "error", err)
				return
			}
		}
	}
}

// handlePacket is the default dispatch logic used by roomConn.
// The client overrides this via the dispatch field.
func handleHeartbeatReply(body []byte) *HeartbeatData {
	if len(body) >= 4 {
		return &HeartbeatData{
			Popularity: binary.BigEndian.Uint32(body[:4]),
		}
	}
	return nil
}

func backoff(attempt int) time.Duration {
	d := baseBackoff * time.Duration(math.Pow(2, float64(attempt-1)))
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

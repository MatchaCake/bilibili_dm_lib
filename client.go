package dm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Client subscribes to danmaku streams from one or more Bilibili live rooms.
type Client struct {
	mu     sync.RWMutex
	config clientConfig
	logger *slog.Logger

	// Typed event callbacks.
	onDanmaku  []func(*Danmaku)
	onGift     []func(*Gift)
	onSuper    []func(*SuperChat)
	onGuard    []func(*GuardBuy)
	onLive     []func(*LiveEvent)
	onPrepare  []func(*LiveEvent)
	onInteract []func(*InteractWord)
	onRaw      []func(cmd string, raw []byte)
	onHeart    []func(*HeartbeatData)

	// Channel-based subscribers.
	subs []chan Event

	// Room management.
	rooms      map[int64]context.CancelFunc // shortRoomID → cancel
	roomsMu    sync.Mutex
	parentCtx  context.Context
	httpClient *http.Client
}

// NewClient creates a new danmaku client.
func NewClient(opts ...Option) *Client {
	cfg := clientConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{
		config:     cfg,
		logger:     slog.Default(),
		rooms:      make(map[int64]context.CancelFunc),
		httpClient: hc,
	}
}

// OnDanmaku registers a callback for chat messages.
func (c *Client) OnDanmaku(fn func(*Danmaku)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onDanmaku = append(c.onDanmaku, fn)
}

// OnGift registers a callback for gift events.
func (c *Client) OnGift(fn func(*Gift)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onGift = append(c.onGift, fn)
}

// OnSuperChat registers a callback for Super Chat messages.
func (c *Client) OnSuperChat(fn func(*SuperChat)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onSuper = append(c.onSuper, fn)
}

// OnGuardBuy registers a callback for guard purchases.
func (c *Client) OnGuardBuy(fn func(*GuardBuy)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onGuard = append(c.onGuard, fn)
}

// OnLive registers a callback for when a room goes live.
func (c *Client) OnLive(fn func(*LiveEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onLive = append(c.onLive, fn)
}

// OnPreparing registers a callback for when a room goes offline.
func (c *Client) OnPreparing(fn func(*LiveEvent)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onPrepare = append(c.onPrepare, fn)
}

// OnInteractWord registers a callback for user interactions (entry, follow, share).
func (c *Client) OnInteractWord(fn func(*InteractWord)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onInteract = append(c.onInteract, fn)
}

// OnRawEvent registers a catch-all callback for any command event.
// This receives events that are not parsed into typed structs.
func (c *Client) OnRawEvent(fn func(cmd string, raw []byte)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onRaw = append(c.onRaw, fn)
}

// OnHeartbeat registers a callback for heartbeat reply (popularity) events.
func (c *Client) OnHeartbeat(fn func(*HeartbeatData)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onHeart = append(c.onHeart, fn)
}

// Subscribe returns a channel that receives all events.
// The channel is buffered (256). The caller should consume events
// promptly to avoid blocking. The channel is closed when the client stops.
func (c *Client) Subscribe() <-chan Event {
	ch := make(chan Event, 256)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subs = append(c.subs, ch)
	return ch
}

// Start connects to all configured rooms and blocks until ctx is cancelled.
func (c *Client) Start(ctx context.Context) error {
	c.parentCtx = ctx

	if len(c.config.roomIDs) == 0 {
		return fmt.Errorf("no rooms configured; use WithRoomID or AddRoom")
	}

	var wg sync.WaitGroup
	for _, id := range c.config.roomIDs {
		wg.Add(1)
		go func(roomID int64) {
			defer wg.Done()
			c.startRoom(ctx, roomID)
		}(id)
	}

	<-ctx.Done()
	wg.Wait()

	// Close subscriber channels.
	c.mu.Lock()
	for _, ch := range c.subs {
		close(ch)
	}
	c.subs = nil
	c.mu.Unlock()

	return ctx.Err()
}

// AddRoom dynamically adds a room to the client. Safe to call after Start.
func (c *Client) AddRoom(roomID int64) error {
	if c.parentCtx == nil {
		// Not yet started — just add to config.
		c.config.roomIDs = append(c.config.roomIDs, roomID)
		return nil
	}

	c.roomsMu.Lock()
	if _, exists := c.rooms[roomID]; exists {
		c.roomsMu.Unlock()
		return fmt.Errorf("room %d already connected", roomID)
	}
	c.roomsMu.Unlock()

	go c.startRoom(c.parentCtx, roomID)
	return nil
}

// RemoveRoom disconnects from a room.
func (c *Client) RemoveRoom(roomID int64) {
	c.roomsMu.Lock()
	defer c.roomsMu.Unlock()
	if cancel, ok := c.rooms[roomID]; ok {
		cancel()
		delete(c.rooms, roomID)
	}
}

func (c *Client) startRoom(ctx context.Context, roomID int64) {
	roomCtx, cancel := context.WithCancel(ctx)

	c.roomsMu.Lock()
	c.rooms[roomID] = cancel
	c.roomsMu.Unlock()

	defer func() {
		c.roomsMu.Lock()
		delete(c.rooms, roomID)
		c.roomsMu.Unlock()
	}()

	cookies := ""
	if c.config.sessdata != "" {
		cookies = fmt.Sprintf("SESSDATA=%s; bili_jct=%s", c.config.sessdata, c.config.biliJCT)
	}

	rc := &roomConn{
		shortRoomID: roomID,
		httpClient:  c.httpClient,
		cookies:     cookies,
		dispatch:    c.dispatchPacket,
		logger:      c.logger,
	}
	rc.run(roomCtx)
}

// dispatchPacket routes a decoded packet to the appropriate handlers.
func (c *Client) dispatchPacket(roomID int64, pkt *Packet) {
	switch pkt.OpType {
	case OpHeartbeatReply:
		hb := handleHeartbeatReply(pkt.Body)
		if hb != nil {
			c.mu.RLock()
			for _, fn := range c.onHeart {
				fn(hb)
			}
			c.mu.RUnlock()
			c.publishEvent(Event{RoomID: roomID, Type: EventHeartbeat, Data: hb})
		}

	case OpCertificateResp:
		// Auth response — just log it.
		c.logger.Info("authenticated", "room", roomID)

	case OpCommand:
		c.dispatchCommand(roomID, pkt.Body)
	}
}

func (c *Client) dispatchCommand(roomID int64, body []byte) {
	event := parseCommandPacket(roomID, body)

	// Always fire raw handlers.
	cmd := extractCMD(body)
	c.mu.RLock()
	for _, fn := range c.onRaw {
		fn(cmd, body)
	}
	c.mu.RUnlock()

	if event == nil {
		// Unrecognised command — raw handlers already called.
		c.publishEvent(Event{RoomID: roomID, Type: EventRaw, Data: body})
		return
	}

	// Dispatch to typed handlers.
	c.mu.RLock()
	switch d := event.Data.(type) {
	case *Danmaku:
		for _, fn := range c.onDanmaku {
			fn(d)
		}
	case *Gift:
		for _, fn := range c.onGift {
			fn(d)
		}
	case *SuperChat:
		for _, fn := range c.onSuper {
			fn(d)
		}
	case *GuardBuy:
		for _, fn := range c.onGuard {
			fn(d)
		}
	case *LiveEvent:
		if d.Live {
			for _, fn := range c.onLive {
				fn(d)
			}
		} else {
			for _, fn := range c.onPrepare {
				fn(d)
			}
		}
	case *InteractWord:
		for _, fn := range c.onInteract {
			fn(d)
		}
	}
	c.mu.RUnlock()

	c.publishEvent(*event)
}

func (c *Client) publishEvent(ev Event) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, ch := range c.subs {
		select {
		case ch <- ev:
		default:
			// Channel full — drop to avoid blocking.
		}
	}
}

// extractCMD pulls the "cmd" field from a raw JSON command body.
func extractCMD(body []byte) string {
	// Fast path: avoid full JSON parse.
	var partial struct {
		CMD string `json:"cmd"`
	}
	if len(body) > 4 {
		// Ignore error — best effort.
		_ = json.Unmarshal(body, &partial)
	}
	return partial.CMD
}


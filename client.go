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
// It can also send danmaku via the built-in Sender (see SendDanmaku).
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
	parentMu   sync.Mutex // protects parentCtx
	wg         sync.WaitGroup
	httpClient *http.Client

	// Sender (lazily initialised on first SendDanmaku call).
	sender     *Sender
	senderOnce sync.Once
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
	c.parentMu.Lock()
	c.parentCtx = ctx
	c.parentMu.Unlock()

	if len(c.config.roomIDs) == 0 {
		return fmt.Errorf("no rooms configured; use WithRoomID or AddRoom")
	}

	for _, id := range c.config.roomIDs {
		c.wg.Add(1)
		go func(roomID int64) {
			defer c.wg.Done()
			c.startRoom(ctx, roomID)
		}(id)
	}

	<-ctx.Done()
	c.wg.Wait()

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
	c.parentMu.Lock()
	ctx := c.parentCtx
	c.parentMu.Unlock()

	if ctx == nil {
		// Not yet started — just add to config.
		c.roomsMu.Lock()
		c.config.roomIDs = append(c.config.roomIDs, roomID)
		c.roomsMu.Unlock()
		return nil
	}

	c.roomsMu.Lock()
	if _, exists := c.rooms[roomID]; exists {
		c.roomsMu.Unlock()
		return fmt.Errorf("room %d already connected", roomID)
	}
	// Reserve the slot so concurrent AddRoom calls for the same ID are rejected.
	c.rooms[roomID] = nil
	c.roomsMu.Unlock()

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.startRoom(ctx, roomID)
	}()
	return nil
}

// RemoveRoom disconnects from a room.
func (c *Client) RemoveRoom(roomID int64) {
	c.roomsMu.Lock()
	defer c.roomsMu.Unlock()
	if cancel, ok := c.rooms[roomID]; ok {
		if cancel != nil {
			cancel()
		}
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

	cookies := "buvid3=" + generateBuvid3()
	if c.config.sessdata != "" {
		cookies = fmt.Sprintf("SESSDATA=%s; bili_jct=%s; buvid3=%s", c.config.sessdata, c.config.biliJCT, generateBuvid3())
	}

	// Resolve UID if not configured
	uid := c.config.uid
	if uid == 0 && c.config.sessdata != "" {
		if navUID, err := getNavUID(roomCtx, c.httpClient, cookies); err == nil {
			uid = navUID
			c.logger.Info("resolved UID from nav", "uid", uid)
		}
	}

	rc := &roomConn{
		shortRoomID: roomID,
		uid:         uid,
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

// SendDanmaku sends a danmaku message to the given room.
// It uses the Client's credentials (set via WithCookie) and sender settings
// (WithMaxDanmakuLength, WithSendCooldown). Long messages are auto-split.
func (c *Client) SendDanmaku(ctx context.Context, roomID int64, msg string) error {
	c.senderOnce.Do(c.initSender)
	return c.sender.Send(ctx, roomID, msg)
}

func (c *Client) initSender() {
	var senderOpts []SenderOption
	if c.config.sessdata != "" {
		senderOpts = append(senderOpts, WithSenderCookie(c.config.sessdata, c.config.biliJCT))
	}
	if c.config.maxLength > 0 {
		senderOpts = append(senderOpts, WithMaxLength(c.config.maxLength))
	}
	if c.config.cooldown > 0 {
		senderOpts = append(senderOpts, WithCooldown(c.config.cooldown))
	}
	senderOpts = append(senderOpts, WithSenderHTTPClient(c.httpClient))
	c.sender = NewSender(senderOpts...)
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


// generateBuvid3 creates a random buvid3 device identifier.
// Format: UUID v4 + "infoc" (e.g. "1702EE27-7022-473C-8F6B-4BC9DD6AE419infoc")
func generateBuvid3() string {
	b := make([]byte, 16)
	// Use time-based pseudo-random (good enough for device ID)
	now := time.Now().UnixNano()
	for i := range b {
		now = now*6364136223846793005 + 1442695040888963407 // LCG
		b[i] = byte(now >> 32)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 1
	return fmt.Sprintf("%08X-%04X-%04X-%04X-%012Xinfoc",
		uint32(b[0])<<24|uint32(b[1])<<16|uint32(b[2])<<8|uint32(b[3]),
		uint16(b[4])<<8|uint16(b[5]),
		uint16(b[6])<<8|uint16(b[7]),
		uint16(b[8])<<8|uint16(b[9]),
		uint64(b[10])<<40|uint64(b[11])<<32|uint64(b[12])<<24|uint64(b[13])<<16|uint64(b[14])<<8|uint64(b[15]),
	)
}

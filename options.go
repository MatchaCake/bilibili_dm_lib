package dm

import (
	"net/http"
	"time"
)

// Option configures a Client.
type Option func(*clientConfig)

type clientConfig struct {
	roomIDs    []int64
	sessdata   string
	biliJCT    string
	httpClient *http.Client

	// Sender options (used by Client.SendDanmaku).
	maxLength int
	cooldown  time.Duration
}

// WithRoomID adds a room to connect to on Start.
func WithRoomID(roomID int64) Option {
	return func(c *clientConfig) {
		c.roomIDs = append(c.roomIDs, roomID)
	}
}

// WithCookie sets the SESSDATA and bili_jct cookies for authenticated access.
// Authenticated connections receive richer danmaku data (e.g., full medal info).
func WithCookie(sessdata, biliJCT string) Option {
	return func(c *clientConfig) {
		c.sessdata = sessdata
		c.biliJCT = biliJCT
	}
}

// WithHTTPClient overrides the default HTTP client used for API calls.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) {
		c.httpClient = hc
	}
}

// WithMaxDanmakuLength sets the maximum rune length per danmaku message
// for the Client's built-in Sender. Default is 20; UL20+ users can set 30.
func WithMaxDanmakuLength(n int) Option {
	return func(c *clientConfig) {
		c.maxLength = n
	}
}

// WithSendCooldown sets the minimum interval between sends to the same room
// for the Client's built-in Sender. Default is 5 seconds.
func WithSendCooldown(d time.Duration) Option {
	return func(c *clientConfig) {
		c.cooldown = d
	}
}

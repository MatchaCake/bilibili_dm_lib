package dm

import (
	"net/http"
	"time"
)

// DanmakuMode controls how the danmaku is displayed in the live room.
type DanmakuMode int

const (
	ModeScroll DanmakuMode = 1 // scrolling (default)
	ModeBottom DanmakuMode = 4 // pinned at bottom
	ModeTop    DanmakuMode = 5 // pinned at top
)

// SenderOption configures a Sender.
type SenderOption func(*senderConfig)

type senderConfig struct {
	sessdata   string
	biliJCT    string
	maxLength  int
	cooldown   time.Duration
	httpClient *http.Client
}

// WithSenderCookie sets the SESSDATA and bili_jct cookies for sending.
// Both values are required — bili_jct is used as the CSRF token.
func WithSenderCookie(sessdata, biliJCT string) SenderOption {
	return func(c *senderConfig) {
		c.sessdata = sessdata
		c.biliJCT = biliJCT
	}
}

// WithMaxLength sets the maximum rune length per danmaku message.
// Messages exceeding this limit are auto-split into multiple sends.
// Default is 20. Users with UL20+ can set this to 30.
func WithMaxLength(n int) SenderOption {
	return func(c *senderConfig) {
		c.maxLength = n
	}
}

// WithCooldown sets the minimum interval between sends to the same room.
// Default is 5 seconds.
func WithCooldown(d time.Duration) SenderOption {
	return func(c *senderConfig) {
		c.cooldown = d
	}
}

// WithSenderHTTPClient overrides the default HTTP client used by the Sender.
func WithSenderHTTPClient(hc *http.Client) SenderOption {
	return func(c *senderConfig) {
		c.httpClient = hc
	}
}

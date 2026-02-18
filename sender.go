package dm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sendDanmakuURL = "https://api.live.bilibili.com/msg/send"

const (
	defaultMaxLength = 20
	defaultCooldown  = 5 * time.Second
)

// SendError is returned when the Bilibili API responds with a non-zero code.
type SendError struct {
	Code    int
	Message string
}

func (e *SendError) Error() string {
	return fmt.Sprintf("bilibili send error %d: %s", e.Code, e.Message)
}

// Sender sends danmaku messages to Bilibili live rooms.
// It is safe for concurrent use.
type Sender struct {
	config     senderConfig
	logger     *slog.Logger
	httpClient *http.Client

	// Per-room rate limiting: roomID → *time.Time (last send time).
	lastSend sync.Map
}

// NewSender creates a standalone Sender for sending danmaku without subscribing.
func NewSender(opts ...SenderOption) *Sender {
	cfg := senderConfig{
		maxLength: defaultMaxLength,
		cooldown:  defaultCooldown,
	}
	for _, o := range opts {
		o(&cfg)
	}

	hc := cfg.httpClient
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	return &Sender{
		config:     cfg,
		logger:     slog.Default(),
		httpClient: hc,
	}
}

// Send sends a danmaku message to the given room using the default scroll mode.
// Long messages are automatically split into chunks of maxLength runes,
// with cooldown pauses between each chunk.
func (s *Sender) Send(ctx context.Context, roomID int64, msg string) error {
	return s.SendWithMode(ctx, roomID, msg, ModeScroll)
}

// SendWithMode sends a danmaku message with the specified display mode.
func (s *Sender) SendWithMode(ctx context.Context, roomID int64, msg string, mode DanmakuMode) error {
	if s.config.sessdata == "" || s.config.biliJCT == "" {
		return fmt.Errorf("cookie required: call WithSenderCookie (or WithCookie on Client) before sending")
	}

	chunks := splitMessage(msg, s.config.maxLength)
	for i, chunk := range chunks {
		if err := s.waitCooldown(ctx, roomID); err != nil {
			return err
		}
		if err := s.sendOne(ctx, roomID, chunk, mode); err != nil {
			return fmt.Errorf("chunk %d/%d: %w", i+1, len(chunks), err)
		}
	}
	return nil
}

// waitCooldown blocks until the per-room cooldown has elapsed.
func (s *Sender) waitCooldown(ctx context.Context, roomID int64) error {
	now := time.Now()
	if v, ok := s.lastSend.Load(roomID); ok {
		last := v.(time.Time)
		wait := s.config.cooldown - now.Sub(last)
		if wait > 0 {
			s.logger.Debug("rate limit wait", "room", roomID, "wait", wait)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	return nil
}

// sendOne sends a single danmaku message (no splitting, no cooldown check).
func (s *Sender) sendOne(ctx context.Context, roomID int64, msg string, mode DanmakuMode) error {
	form := url.Values{
		"bubble":     {"0"},
		"msg":        {msg},
		"color":      {"16777215"},
		"mode":       {strconv.Itoa(int(mode))},
		"fontsize":   {"25"},
		"rnd":        {strconv.FormatInt(time.Now().Unix(), 10)},
		"roomid":     {strconv.FormatInt(roomID, 10)},
		"csrf":       {s.config.biliJCT},
		"csrf_token": {s.config.biliJCT},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendDanmakuURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	setCommonHeaders(req, fmt.Sprintf("SESSDATA=%s; bili_jct=%s", s.config.sessdata, s.config.biliJCT))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read send response: %w", err)
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Msg     string `json:"msg"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("parse send response: %w", err)
	}

	// Record send time for rate limiting (even on failure, to avoid hammering).
	s.lastSend.Store(roomID, time.Now())

	if result.Code != 0 {
		msg := result.Message
		if msg == "" {
			msg = result.Msg
		}
		return &SendError{Code: result.Code, Message: msg}
	}

	s.logger.Debug("danmaku sent", "room", roomID, "msg", msg)
	return nil
}

// splitMessage breaks a message into chunks of at most maxLen runes.
func splitMessage(msg string, maxLen int) []string {
	runes := []rune(msg)
	if len(runes) <= maxLen {
		return []string{msg}
	}

	var chunks []string
	for len(runes) > 0 {
		end := maxLen
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, string(runes[:end]))
		runes = runes[end:]
	}
	return chunks
}

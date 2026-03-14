package dm

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSenderSerializesConcurrentSendsPerRoom(t *testing.T) {
	t.Parallel()

	var (
		mu    sync.Mutex
		calls []time.Time
	)

	sender := NewSender(
		WithSenderCookie("sess", "csrf"),
		WithCooldown(80*time.Millisecond),
		WithSenderHTTPClient(&http.Client{
			Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
				mu.Lock()
				calls = append(calls, time.Now())
				mu.Unlock()

				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"code":0}`)),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)

	start := make(chan struct{})
	errCh := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errCh <- sender.Send(context.Background(), 1, "hello")
		}()
	}
	close(start)

	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("Send() error = %v", err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 HTTP calls, got %d", len(calls))
	}

	gap := calls[1].Sub(calls[0])
	if gap < 70*time.Millisecond {
		t.Fatalf("expected cooldown gap, got %v", gap)
	}
}

func TestClientAddRoomRejectsDuplicateBeforeStart(t *testing.T) {
	t.Parallel()

	client := NewClient(WithRoomID(100))
	if err := client.AddRoom(100); err == nil {
		t.Fatal("expected duplicate room error")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

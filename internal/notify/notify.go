// Package notify sends the configured webhook when stock is detected.
package notify

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Notifier sends a notification to a trigger URL.
type Notifier interface {
	Send(ctx context.Context, trigger string) error
}

// HTTPNotifier POSTs to the trigger URL. Bark, PushDeer, ntfy and most
// self-hosted webhook bridges accept this shape.
type HTTPNotifier struct {
	client *http.Client
	logger *slog.Logger
}

func NewHTTPNotifier(timeout time.Duration, logger *slog.Logger) *HTTPNotifier {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPNotifier{
		client: &http.Client{Timeout: timeout},
		logger: logger,
	}
}

func (n *HTTPNotifier) Send(ctx context.Context, trigger string) error {
	const attempts = 3
	var lastErr error

	for attempt := 0; attempt < attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, trigger, nil)
		if err != nil {
			return err
		}
		req.Header.Set("user-agent", "apple-store-watcher")
		req.Header.Set("content-type", "text/plain; charset=utf-8")

		resp, err := n.client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("notification endpoint returned %s: %s", resp.Status, string(body))
			if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
				return lastErr
			}
		}

		if attempt+1 < attempts {
			delay := time.Duration(300*(1<<attempt)) * time.Millisecond
			n.logger.Debug("notification failed, retrying", "attempt", attempt+1, "delay", delay, "error", lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return lastErr
}

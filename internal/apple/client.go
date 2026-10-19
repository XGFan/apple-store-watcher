// Package apple contains the client used to talk to Apple's public store
// endpoints. The endpoints used here are the ones the Apple Store website
// itself uses (2026), not the legacy rsp-web / fulfillment-messages APIs.
package apple

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the mainland-China Apple Store site.
	DefaultBaseURL = "https://www.apple.com.cn"

	defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
)

// Client is a small HTTP client with retries and the headers Apple expects.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	UserAgent  string
	Logger     *slog.Logger
}

// NewClient returns a client with a request timeout and sane defaults.
func NewClient(timeout time.Duration, logger *slog.Logger) *Client {
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: timeout},
		UserAgent:  defaultUserAgent,
		Logger:     logger,
	}
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	return strings.TrimRight(c.BaseURL, "/") + path
}

const maxAttempts = 3

func (c *Client) do(ctx context.Context, method, rawURL string, body []byte, headers map[string]string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var reader io.Reader
		if body != nil {
			reader = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, c.url(rawURL), reader)
		if err != nil {
			return nil, err
		}

		req.Header.Set("user-agent", c.UserAgent)
		req.Header.Set("accept", "application/json, text/plain, */*")
		req.Header.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
		req.Header.Set("referer", c.url("/shop/buy-iphone"))
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		resp, err := c.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
		} else {
			data, readErr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return data, nil
			} else {
				lastErr = fmt.Errorf("%s %s: %s: %s", method, rawURL, resp.Status, snippet(data))
				if !retryableStatus(resp.StatusCode) {
					return nil, lastErr
				}
			}
		}

		if attempt+1 < maxAttempts {
			delay := time.Duration(400*(1<<attempt)+rand.Intn(250)) * time.Millisecond
			c.Logger.Debug("request failed, retrying", "attempt", attempt+1, "delay", delay, "error", lastErr)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return nil, lastErr
}

func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 180 {
		return s[:180] + "..."
	}
	return s
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, path, nil, nil)
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	data, err := c.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	data, err := c.do(ctx, http.MethodPost, path, body, map[string]string{
		"content-type": "application/json",
		"accept":       "application/json",
	})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func withQuery(path string, values url.Values) string {
	return path + "?" + values.Encode()
}

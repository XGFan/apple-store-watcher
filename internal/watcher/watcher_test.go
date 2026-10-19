package watcher

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apple-store-watcher/internal/apple"
	"apple-store-watcher/internal/config"
	"apple-store-watcher/internal/notify"
)

type fakeNotifier struct {
	sent int
}

func (f *fakeNotifier) Send(context.Context, string) error {
	f.sent++
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestHandleResultNotifiesOnTransitionAndCooldown(t *testing.T) {
	notifier := &fakeNotifier{}
	logger := discardLogger()
	opts := Options{NotifyInterval: time.Hour}
	inStock := false
	var lastNotified time.Time

	rule := config.Rule{Store: "R389", SKU: []string{"MK2Q4CH/A"}, Trigger: "https://example.com"}
	available := &apple.Availability{
		Store:     "R389",
		Available: true,
		Parts:     []apple.PartAvailability{{PartNumber: "MK2Q4CH/A", Available: true, AvailableStores: 1}},
	}
	soldOut := &apple.Availability{
		Store: "R389",
		Parts: []apple.PartAvailability{{PartNumber: "MK2Q4CH/A"}},
	}

	handleResult(context.Background(), rule, available, notifier, opts, logger, &inStock, &lastNotified)
	if notifier.sent != 1 {
		t.Fatalf("sent = %d, want 1", notifier.sent)
	}

	// Still in stock: no duplicate notification inside the cooldown.
	handleResult(context.Background(), rule, available, notifier, opts, logger, &inStock, &lastNotified)
	if notifier.sent != 1 {
		t.Fatalf("sent = %d, want 1", notifier.sent)
	}

	// Sold out, then back in stock: notify immediately.
	handleResult(context.Background(), rule, soldOut, notifier, opts, logger, &inStock, &lastNotified)
	if inStock {
		t.Fatal("expected state to be out of stock")
	}
	handleResult(context.Background(), rule, available, notifier, opts, logger, &inStock, &lastNotified)
	if notifier.sent != 2 {
		t.Fatalf("sent = %d, want 2", notifier.sent)
	}
}

func TestRenderTrigger(t *testing.T) {
	got := renderTrigger("https://example.com/?store={store}&sku={sku}", "R389", []string{"MK2Q4CH/A", "MK2P4CH/A"}, nil)
	want := "https://example.com/?store=R389&sku=MK2Q4CH%2FA%2CMK2P4CH%2FA"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderTriggerNamePlaceholder(t *testing.T) {
	names := map[string]string{
		"MK2Q4CH/A": "iPhone Duo - 夜空色 - 512GB",
		"MK2P4CH/A": "iPhone Duo - 星光白色 - 512GB",
	}

	got := renderTrigger(
		"https://example.com/?store={store}&name={name}&sku={sku}",
		"R389",
		[]string{"MK2Q4CH/A", "MK2P4CH/A"},
		names,
	)
	want := "https://example.com/?store=R389&name=iPhone+Duo+-+%E5%A4%9C%E7%A9%BA%E8%89%B2+-+512GB%2C+iPhone+Duo+-+%E6%98%9F%E5%85%89%E7%99%BD%E8%89%B2+-+512GB&sku=MK2Q4CH%2FA%2CMK2P4CH%2FA"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderTriggerNameFallsBackToPart(t *testing.T) {
	got := renderTrigger("https://example.com/?name={name}", "R389", []string{"MK2Q4CH/A"}, map[string]string{})
	want := "https://example.com/?name=MK2Q4CH%2FA"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRenderTriggerNamesWithBracesAreNotReplacedTwice(t *testing.T) {
	names := map[string]string{"SKU1": "odd {sku} name"}
	got := renderTrigger("https://example.com/?name={name}&sku={sku}", "R389", []string{"SKU1"}, names)
	if strings.Contains(got, "%7Bsku%7D") == false {
		t.Fatalf("name containing a placeholder was re-expanded: %q", got)
	}
}

func TestHandleResultSendsProductNameInTrigger(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query().Get("name")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	opts := Options{
		NotifyInterval: time.Hour,
		ProductNames:   map[string]string{"MK2Q4CH/A": "iPhone Duo - 夜空色 - 512GB"},
	}
	rule := config.Rule{
		Store:   "R389",
		SKU:     []string{"MK2Q4CH/A"},
		Trigger: server.URL + "/notify?name={name}&sku={sku}&store={store}",
	}
	result := &apple.Availability{
		Store:     "R389",
		Available: true,
		Parts:     []apple.PartAvailability{{PartNumber: "MK2Q4CH/A", Available: true}},
	}

	inStock := false
	var lastNotified time.Time
	handleResult(context.Background(), rule, result, notify.NewHTTPNotifier(time.Second, discardLogger()), opts, discardLogger(), &inStock, &lastNotified)

	if want := "iPhone Duo - 夜空色 - 512GB"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
}

func TestRunAppliesConfigIntervalsWhenOptionsEmpty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := &config.Config{
		Interval:       time.Minute,
		NotifyInterval: time.Hour,
		Rules:          []config.Rule{{Store: "R389", SKU: []string{"MK2Q4CH/A"}, Trigger: "https://example.com"}},
	}

	done := make(chan struct{})
	go func() {
		// An empty Options must fall back to the config values instead of
		// spinning with a zero interval.
		Run(ctx, cfg, fakeChecker{}, &fakeNotifier{}, Options{})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

type fakeChecker struct{}

func (fakeChecker) CheckPickup(context.Context, string, []string) (*apple.Availability, error) {
	return &apple.Availability{}, nil
}

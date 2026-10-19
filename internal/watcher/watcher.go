// Package watcher runs the polling loop for each configured rule.
package watcher

import (
	"context"
	"log/slog"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"

	"apple-store-watcher/internal/apple"
	"apple-store-watcher/internal/config"
	"apple-store-watcher/internal/notify"
)

// Checker checks pickup availability at one store.
type Checker interface {
	CheckPickup(ctx context.Context, store string, parts []string) (*apple.Availability, error)
}

// Options controls the polling loop.
type Options struct {
	Interval       time.Duration
	NotifyInterval time.Duration
	// ProductNames maps part numbers to human readable names. It is used to
	// expand the {name} placeholder in trigger URLs; unknown parts fall back to
	// the part number.
	ProductNames map[string]string
	Logger       *slog.Logger
}

// Run starts one goroutine per rule and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg *config.Config, checker Checker, notifier notify.Notifier, opts Options) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Interval <= 0 {
		opts.Interval = cfg.Interval
	}
	if opts.NotifyInterval <= 0 {
		opts.NotifyInterval = cfg.NotifyInterval
	}

	var wg sync.WaitGroup
	for _, rule := range cfg.Rules {
		wg.Add(1)
		go func(rule config.Rule) {
			defer wg.Done()
			runRule(ctx, rule, checker, notifier, opts)
		}(rule)
	}
	wg.Wait()
}

func runRule(ctx context.Context, rule config.Rule, checker Checker, notifier notify.Notifier, opts Options) {
	log := opts.Logger.With("store", rule.Store, "skus", strings.Join(rule.SKU, ","))

	inStock := false
	var lastNotified time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		start := time.Now()
		result, err := checker.CheckPickup(ctx, rule.Store, rule.SKU)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn("库存检查失败", "error", err, "elapsed", time.Since(start))
			}
		} else {
			handleResult(ctx, rule, result, notifier, opts, log, &inStock, &lastNotified)
			log.Debug("库存检查完成", "available", result.Available, "elapsed", time.Since(start))
		}

		next := withJitter(opts.Interval)
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(next)
	}
}

func handleResult(
	ctx context.Context,
	rule config.Rule,
	result *apple.Availability,
	notifier notify.Notifier,
	opts Options,
	log *slog.Logger,
	inStock *bool,
	lastNotified *time.Time,
) {
	if result.Available {
		availableParts := availablePartNumbers(result)
		shouldNotify := !*inStock || time.Since(*lastNotified) >= opts.NotifyInterval
		if shouldNotify {
			trigger := renderTrigger(rule.Trigger, rule.Store, availableParts, opts.ProductNames)
			if err := notifier.Send(ctx, trigger); err != nil {
				log.Error("发送通知失败", "error", err)
			} else {
				*lastNotified = time.Now()
				log.Info("有货，已发送通知", "parts", strings.Join(availableParts, ","))
			}
		}
		if !*inStock {
			log.Info("检测到可取货", "parts", strings.Join(availableParts, ","), "stores", availableStoreCount(result))
		}
	} else if *inStock {
		log.Info("库存已售罄，继续监控")
	}
	*inStock = result.Available
}

func availablePartNumbers(result *apple.Availability) []string {
	parts := make([]string, 0, len(result.Parts))
	for _, part := range result.Parts {
		if part.Available {
			parts = append(parts, part.PartNumber)
		}
	}
	if len(parts) == 0 {
		// The API can report availability without a per-part breakdown.
		for _, part := range result.Parts {
			parts = append(parts, part.PartNumber)
		}
	}
	return parts
}

func availableStoreCount(result *apple.Availability) int {
	maxStores := 0
	for _, part := range result.Parts {
		if part.AvailableStores > maxStores {
			maxStores = part.AvailableStores
		}
	}
	return maxStores
}

// renderTrigger replaces {store}, {sku} and {name} placeholders with
// URL-escaped values, so notification URLs can include useful context.
// Values are safe in both query and path segments (e.g. Bark's /{key}/{title}/{body}).
// Unknown product names fall back to their part number.
func renderTrigger(template, store string, parts []string, names map[string]string) string {
	labels := make([]string, 0, len(parts))
	for _, part := range parts {
		if name := strings.TrimSpace(names[part]); name != "" {
			labels = append(labels, name)
			continue
		}
		labels = append(labels, part)
	}

	replacer := strings.NewReplacer(
		"{store}", escapeValue(store),
		"{sku}", escapeValue(strings.Join(parts, ",")),
		"{name}", escapeValue(strings.Join(labels, ", ")),
	)
	return replacer.Replace(template)
}

// escapeValue is QueryEscape with spaces as %20: a "+" would be decoded as a
// space in a query string but stays a literal "+" in a path segment.
func escapeValue(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

func withJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	jitter := float64(d) * 0.1
	return d + time.Duration((rand.Float64()*2-1)*jitter)
}

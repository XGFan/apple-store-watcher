package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadModernConfig(t *testing.T) {
	path := writeConfig(t, `
interval: 45s
notify_interval: 10m
timeout: 8s
rules:
  - store: R389
    trigger: https://example.com/hook?store={store}
    sku:
      - mk2q4ch/a
      - MK2P4CH/A
      - mk2q4ch/a
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 45*time.Second || cfg.NotifyInterval != 10*time.Minute || cfg.Timeout != 8*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
	if len(cfg.Rules) != 1 {
		t.Fatalf("got %d rules", len(cfg.Rules))
	}
	if got := cfg.Rules[0].SKU; len(got) != 2 || got[0] != "MK2Q4CH/A" {
		t.Fatalf("unexpected skus: %v", got)
	}
}

func TestLoadLegacyConfig(t *testing.T) {
	path := writeConfig(t, `
- store: R389
  trigger: https://example.com/hook
  sku:
    - MK2Q4CH/A
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Rules) != 1 || cfg.Rules[0].Store != "R389" {
		t.Fatalf("unexpected rules: %+v", cfg.Rules)
	}
	if cfg.Interval != DefaultInterval {
		t.Fatalf("interval = %s", cfg.Interval)
	}
}

func TestLoadRejectsBadTrigger(t *testing.T) {
	path := writeConfig(t, `
rules:
  - store: R389
    trigger: not-a-url
    sku: [MK2Q4CH/A]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadRejectsDuplicateRules(t *testing.T) {
	path := writeConfig(t, `
rules:
  - store: R389
    trigger: https://example.com/a
    sku: [MK2Q4CH/A]
  - store: R389
    trigger: https://example.com/b
    sku: [MK2Q4CH/A]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error")
	}
}

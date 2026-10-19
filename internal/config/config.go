// Package config loads watcher rules from YAML.
package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Defaults for the global watcher settings.
const (
	DefaultInterval       = 30 * time.Second
	DefaultNotifyInterval = 30 * time.Minute
	DefaultTimeout        = 15 * time.Second
)

// Config is the parsed configuration.
type Config struct {
	Interval       time.Duration
	NotifyInterval time.Duration
	Timeout        time.Duration
	Rules          []Rule
}

// Rule watches one store for one or more part numbers.
type Rule struct {
	Store   string   `yaml:"store"`
	SKU     []string `yaml:"sku"`
	Trigger string   `yaml:"trigger"`
}

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("duration must be a scalar")
	}
	if value.Tag == "!!int" {
		var seconds int64
		if err := value.Decode(&seconds); err != nil {
			return err
		}
		*d = Duration(time.Duration(seconds) * time.Second)
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

type rawConfig struct {
	Interval       Duration  `yaml:"interval"`
	NotifyInterval Duration  `yaml:"notify_interval"`
	Timeout        Duration  `yaml:"timeout"`
	Rules          []rawRule `yaml:"rules"`
}

type rawRule struct {
	Store   string   `yaml:"store"`
	SKU     []string `yaml:"sku"`
	Trigger string   `yaml:"trigger"`
}

// Load reads either the modern map-style config (rules:) or the legacy
// top-level list format used by earlier versions.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw rawConfig
	rawErr := yaml.Unmarshal(data, &raw)
	if rawErr == nil && len(raw.Rules) > 0 {
		return build(raw)
	}

	var legacy []rawRule
	legacyErr := yaml.Unmarshal(data, &legacy)
	if legacyErr == nil && len(legacy) > 0 {
		raw.Rules = legacy
		return build(raw)
	}

	if rawErr != nil {
		return nil, fmt.Errorf("parse %s: %w", path, rawErr)
	}
	if legacyErr != nil {
		return nil, fmt.Errorf("parse %s: %w", path, legacyErr)
	}
	return nil, fmt.Errorf("%s: no rules configured", path)
}

func build(raw rawConfig) (*Config, error) {
	cfg := &Config{
		Interval:       orDefault(time.Duration(raw.Interval), DefaultInterval),
		NotifyInterval: orDefault(time.Duration(raw.NotifyInterval), DefaultNotifyInterval),
		Timeout:        orDefault(time.Duration(raw.Timeout), DefaultTimeout),
	}

	if cfg.Interval < time.Second {
		return nil, fmt.Errorf("interval must be at least 1s")
	}
	if cfg.NotifyInterval <= 0 {
		return nil, fmt.Errorf("notify_interval must be positive")
	}
	if cfg.Timeout < time.Second {
		return nil, fmt.Errorf("timeout must be at least 1s")
	}

	seen := make(map[string]bool)
	for i, rule := range raw.Rules {
		rule.Store = strings.TrimSpace(rule.Store)
		rule.Trigger = strings.TrimSpace(rule.Trigger)
		if rule.Store == "" {
			return nil, fmt.Errorf("rules[%d]: store is required", i)
		}
		if len(rule.SKU) == 0 {
			return nil, fmt.Errorf("rules[%d]: sku is required", i)
		}
		if rule.Trigger == "" {
			return nil, fmt.Errorf("rules[%d]: trigger is required", i)
		}
		parsed, err := url.Parse(rule.Trigger)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return nil, fmt.Errorf("rules[%d]: trigger must be an http(s) URL", i)
		}

		skus := make([]string, 0, len(rule.SKU))
		seenSKU := make(map[string]bool)
		for _, sku := range rule.SKU {
			sku = strings.ToUpper(strings.TrimSpace(sku))
			if sku == "" || seenSKU[sku] {
				continue
			}
			seenSKU[sku] = true
			skus = append(skus, sku)
		}
		if len(skus) == 0 {
			return nil, fmt.Errorf("rules[%d]: sku is required", i)
		}
		rule.SKU = skus

		key := rule.Store + "|" + strings.Join(skus, ",")
		if seen[key] {
			return nil, fmt.Errorf("rules[%d]: duplicate rule for store %s and skus %s", i, rule.Store, strings.Join(skus, ", "))
		}
		seen[key] = true
		cfg.Rules = append(cfg.Rules, Rule(rule))
	}

	return cfg, nil
}

func orDefault(value, fallback time.Duration) time.Duration {
	if value <= 0 {
		return fallback
	}
	return value
}

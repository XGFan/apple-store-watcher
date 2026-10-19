package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"apple-store-watcher/internal/apple"
	"apple-store-watcher/internal/config"
	"apple-store-watcher/internal/notify"
	"apple-store-watcher/internal/watcher"
)

func main() {
	if err := run(); err != nil {
		slog.Error("启动失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	printStore := flag.Bool("store", false, "打印当前 Apple Store 门店列表")
	printProduct := flag.Bool("product", false, "打印当前商品列表")
	configPath := flag.String("c", "config.yaml", "配置文件路径")
	family := flag.String("family", "iphone", "商品品类: iphone,ipad,watch,all")
	verbose := flag.Bool("v", false, "输出详细日志")
	once := flag.Bool("once", false, "只检查一次，不发送通知")
	flag.Parse()

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	client := apple.NewClient(20*time.Second, logger)

	if *printStore {
		stores, err := client.Stores(context.Background())
		if err != nil {
			return err
		}
		printMap(stores, func(name, code string) string {
			return fmt.Sprintf("%s - %s", name, code)
		})
		return nil
	}

	if *printProduct {
		categories := strings.Split(*family, ",")
		if strings.EqualFold(strings.TrimSpace(*family), "all") {
			categories = apple.ProductCategories
		}
		products, err := client.Products(context.Background(), categories)
		if err != nil {
			return err
		}
		printMap(products, func(name, part string) string {
			return fmt.Sprintf("%s - %s", name, part)
		})
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	// Use the configured timeout for all network calls.
	client = apple.NewClient(cfg.Timeout, logger)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *verbose {
		logResolvedRules(ctx, client, cfg, logger)
	}

	if *once {
		return checkOnce(ctx, client, cfg, logger)
	}

	productNames := resolveProductNames(ctx, client, cfg, logger)

	logger.Info("开始监控",
		"rules", len(cfg.Rules),
		"interval", cfg.Interval.String(),
		"notify_interval", cfg.NotifyInterval.String(),
	)

	notifier := notify.NewHTTPNotifier(cfg.Timeout, logger)
	watcher.Run(ctx, cfg, client, notifier, watcher.Options{
		ProductNames: productNames,
		Logger:       logger,
	})
	logger.Info("已停止")
	return nil
}

// resolveProductNames loads product names only when a trigger uses {name}.
func resolveProductNames(ctx context.Context, client *apple.Client, cfg *config.Config, logger *slog.Logger) map[string]string {
	needed := false
	for _, rule := range cfg.Rules {
		if strings.Contains(rule.Trigger, "{name}") {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}

	products, err := client.Products(ctx, apple.ProductCategories)
	if err != nil {
		logger.Warn("无法加载商品名称，通知中将使用 SKU 代替 {name}", "error", err)
		return nil
	}
	logger.Debug("已加载商品名称，用于 {name} 占位符", "count", len(products))
	return products
}

func checkOnce(ctx context.Context, client *apple.Client, cfg *config.Config, logger *slog.Logger) error {
	var failed bool
	for _, rule := range cfg.Rules {
		result, err := client.CheckPickup(ctx, rule.Store, rule.SKU)
		if err != nil {
			logger.Error("检查失败", "store", rule.Store, "error", err)
			failed = true
			continue
		}
		for _, part := range result.Parts {
			status := "无货"
			if part.Available {
				status = "可取货"
			}
			fmt.Printf("%s %s %s (有货门店: %d)\n", rule.Store, part.PartNumber, status, part.AvailableStores)
		}
	}
	if failed {
		return errors.New("部分规则检查失败")
	}
	return nil
}

func logResolvedRules(ctx context.Context, client *apple.Client, cfg *config.Config, logger *slog.Logger) {
	stores, err := client.Stores(ctx)
	if err != nil {
		logger.Warn("无法加载门店名称", "error", err)
	}
	products, err := client.Products(ctx, apple.ProductCategories)
	if err != nil {
		logger.Warn("无法加载商品名称", "error", err)
	}
	for _, rule := range cfg.Rules {
		logger.Info("规则", "store", stores[rule.Store], "code", rule.Store)
		for _, sku := range rule.SKU {
			logger.Info("商品", "name", products[sku], "sku", sku)
		}
	}
}

func printMap(values map[string]string, format func(name, code string) string) {
	lines := make([]string, 0, len(values))
	for code, name := range values {
		lines = append(lines, format(name, code))
	}
	sort.Strings(lines)
	for _, line := range lines {
		fmt.Println(line)
	}
}

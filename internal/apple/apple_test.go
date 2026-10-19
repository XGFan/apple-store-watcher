package apple

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestStoresFromRetailPage(t *testing.T) {
	page, err := os.ReadFile("testdata/storelist.html")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/retail/storelist" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(page)
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	stores, err := client.storesFromRetailPage(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := stores["R389"]; got != "上海 - 浦东" {
		t.Fatalf("R389 = %q", got)
	}
	if _, ok := stores["R001"]; ok {
		t.Fatal("non zh_CN store leaked into result")
	}
	if len(stores) != 3 {
		t.Fatalf("got %d stores, want 3", len(stores))
	}
}

func TestStoresFallsBackWhenGraphQLFails(t *testing.T) {
	page, err := os.ReadFile("testdata/storelist.html")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api-www/graphql":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/retail/storelist":
			_, _ = w.Write(page)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	stores, err := client.Stores(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if stores["R401"] != "上海 - 上海环贸 iapm" {
		t.Fatalf("R401 = %q", stores["R401"])
	}
}

func TestExtractProductSelection(t *testing.T) {
	page, err := os.ReadFile("testdata/duo.html")
	if err != nil {
		t.Fatal(err)
	}

	selection, err := extractProductSelection(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Products) != 2 {
		t.Fatalf("got %d products, want 2", len(selection.Products))
	}

	if got := displayValue(selection, "dimensionColor", "nightsky"); got != "夜空色" {
		t.Fatalf("color = %q", got)
	}
	if got := displayValue(selection, "dimensionCapacity", "512gb"); got != "512GB" {
		t.Fatalf("capacity = %q", got)
	}
	if got := displayValue(selection, "dimensionCapacity", "2tb"); got != "2TB" {
		t.Fatalf("capacity = %q", got)
	}
	if got := pageTitle(page); got != "iPhone Duo" {
		t.Fatalf("title = %q", got)
	}
}

func TestProductsFromFamilies(t *testing.T) {
	page, err := os.ReadFile("testdata/duo.html")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/shop/buy-iphone":
			_, _ = w.Write([]byte(`<a href="https://www.apple.com.cn/shop/buy-iphone/iphone-duo">Duo</a>`))
		case "/shop/buy-iphone/iphone-duo":
			_, _ = w.Write(page)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	products, err := client.Products(context.Background(), []string{"iphone"})
	if err != nil {
		t.Fatal(err)
	}
	if got := products["MK2Q4CH/A"]; got != "iPhone Duo - 夜空色 - 512GB" {
		t.Fatalf("product = %q", got)
	}
}

func TestCheckPickupEligibleStores(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shop/sba/availability-message" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("fae") != "true" {
			t.Errorf("missing fae=true")
		}
		if r.URL.Query().Get("parts.0") != "MK2Q4CH/A" {
			t.Errorf("parts.0 = %q", r.URL.Query().Get("parts.0"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"head": map[string]any{"status": "200"},
			"body": map[string]any{
				"pickupEnabled": true,
				"content": []any{
					map[string]any{
						"partNumber":               "MK2Q4CH/A",
						"partAvailableStoresCount": 2,
						"availableAtAnyStore":      true,
						"eligibleStores":           "R401, R389",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	result, err := client.CheckPickup(context.Background(), "R389", []string{"mk2q4ch/a"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatal("expected stock at R389")
	}
	if len(result.Parts) != 1 || !result.Parts[0].Available {
		t.Fatalf("unexpected parts: %+v", result.Parts)
	}

	result, err = client.CheckPickup(context.Background(), "R575", []string{"MK2Q4CH/A"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Available {
		t.Fatal("did not expect stock at R575")
	}
}

func TestCheckPickupPickupMessageFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"head":{"status":"200"},
			"body":{"pickupEnabled":true,"content":[{
				"partNumber":"MK2Q4CH/A",
				"partAvailableStoresCount":1,
				"availableAtAnyStore":true,
				"pickupMessage":{"storeId":"R389","messageTypes":{"regular":{"storeSelectionEnabled":true}}}
			}]}
		}`))
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	result, err := client.CheckPickup(context.Background(), "R389", []string{"MK2Q4CH/A"})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Available {
		t.Fatal("expected pickupMessage fallback to detect stock")
	}
}

func TestCheckPickupErrorsOnEmptyContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"head":{"status":"200"},"body":{"content":[]}}`))
	}))
	defer server.Close()

	client := NewClient(time.Second, testLogger())
	client.BaseURL = server.URL

	_, err := client.CheckPickup(context.Background(), "R389", []string{"MK2Q4CH/A"})
	if err == nil || !strings.Contains(err.Error(), "no content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

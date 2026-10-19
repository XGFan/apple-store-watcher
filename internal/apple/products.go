package apple

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"sync"
)

// ProductCategories are the categories supported by the product browser.
var ProductCategories = []string{"iphone", "ipad", "watch"}

var categoryPaths = map[string]string{
	"iphone": "/shop/buy-iphone",
	"ipad":   "/shop/buy-ipad",
	"watch":  "/shop/buy-watch",
}

// Products returns a map of part number (e.g. MK2Q4CH/A) to a human readable
// name (e.g. "iPhone Duo - 夜空色 - 512GB").
//
// The catalog is read from the same buy-flow pages the website uses, so new
// products such as iPhone Duo show up without shipping new code or bundled
// JSON snapshots.
func (c *Client) Products(ctx context.Context, categories []string) (map[string]string, error) {
	categories = normalizeCategories(categories)

	type family struct {
		category string
		slug     string
	}

	var families []family
	seen := make(map[string]bool)
	for _, category := range categories {
		path, ok := categoryPaths[category]
		if !ok {
			return nil, fmt.Errorf("unknown product category %q (want one of %s)", category, strings.Join(ProductCategories, ", "))
		}
		links, err := c.familyLinks(ctx, path, category)
		if err != nil {
			c.Logger.Warn("cannot list product families", "category", category, "error", err)
			continue
		}
		for _, slug := range links {
			key := category + "/" + slug
			if seen[key] {
				continue
			}
			seen[key] = true
			families = append(families, family{category: category, slug: slug})
		}
	}

	if len(families) == 0 {
		return nil, fmt.Errorf("no product families found")
	}

	type result struct {
		products map[string]string
		err      error
	}

	results := make(chan result, len(families))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup

	for _, f := range families {
		wg.Add(1)
		go func(f family) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results <- result{err: ctx.Err()}
				return
			}

			path := categoryPaths[f.category] + "/" + f.slug
			products, err := c.familyProducts(ctx, path, f.slug)
			results <- result{products: products, err: err}
		}(f)
	}

	wg.Wait()
	close(results)

	merged := make(map[string]string)
	var firstErr error
	for res := range results {
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			c.Logger.Debug("skipping product family", "error", res.err)
			continue
		}
		for part, name := range res.products {
			merged[part] = name
		}
	}

	if len(merged) == 0 {
		if firstErr != nil {
			return nil, firstErr
		}
		return nil, fmt.Errorf("no products found")
	}
	return merged, nil
}

func normalizeCategories(categories []string) []string {
	if len(categories) == 0 {
		return []string{"iphone"}
	}
	var out []string
	seen := make(map[string]bool)
	for _, category := range categories {
		category = strings.ToLower(strings.TrimSpace(category))
		if category == "" || seen[category] {
			continue
		}
		seen[category] = true
		out = append(out, category)
	}
	if len(out) == 0 {
		return []string{"iphone"}
	}
	return out
}

var familyLinkPattern = regexp.MustCompile(`(?:https?://[a-z0-9.-]+)?/shop/buy-(iphone|ipad|watch)/([a-z0-9][a-z0-9-]*)`)

var ignoredFamilySlugs = map[string]bool{
	"carrier-offers":   true,
	"compare":          true,
	"trade-in":         true,
	"applecare":        true,
	"carrier":          true,
	"shop-help":        true,
	"special-deals":    true,
	"refurbished":      true,
	"education-store":  true,
	"business":         true,
	"government":       true,
	"veterans-affairs": true,
}

func (c *Client) familyLinks(ctx context.Context, path, category string) ([]string, error) {
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	var slugs []string
	seen := make(map[string]bool)
	for _, m := range familyLinkPattern.FindAllSubmatch(data, -1) {
		if string(m[1]) != category {
			continue
		}
		slug := string(m[2])
		if ignoredFamilySlugs[slug] || seen[slug] {
			continue
		}
		seen[slug] = true
		slugs = append(slugs, slug)
	}
	return slugs, nil
}

type productSelectionData struct {
	Sections []struct {
		FormFieldName string `json:"formFieldName"`
	} `json:"sections"`
	DisplayValues map[string]map[string]any `json:"displayValues"`
	Products      []map[string]any          `json:"products"`
}

func (c *Client) familyProducts(ctx context.Context, path, fallbackName string) (map[string]string, error) {
	data, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}

	selection, err := extractProductSelection(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	name := pageTitle(data)
	if name == "" {
		name = strings.ReplaceAll(fallbackName, "-", " ")
	}

	products := make(map[string]string, len(selection.Products))
	for _, raw := range selection.Products {
		part := stringValue(raw, "partNumber")
		if part == "" {
			// Apple Watch pages use "part" instead of "partNumber".
			part = stringValue(raw, "part")
		}
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		nested, _ := raw["dimensions"].(map[string]any)
		dimensions := make([]string, 0, len(selection.Sections))
		for _, section := range selection.Sections {
			value := stringValue(raw, section.FormFieldName)
			if value == "" && nested != nil {
				value = stringValue(nested, section.FormFieldName)
			}
			if value == "" {
				continue
			}
			dimensions = append(dimensions, displayValue(selection, section.FormFieldName, value))
		}

		label := name
		if len(dimensions) > 0 {
			label += " - " + strings.Join(dimensions, " - ")
		}
		products[part] = label
	}
	if len(products) == 0 {
		return nil, fmt.Errorf("%s: no products in buy-flow payload", path)
	}
	return products, nil
}

func displayValue(selection *productSelectionData, dimension, value string) string {
	// Screensize display values are long marketing sentences
	// ("iPhone 16 6.1 英寸显示屏配备 128GB 存储容量"), so prefer the raw value.
	if dimension == "dimensionScreensize" {
		return formatRawDimension(value)
	}
	if entries, ok := selection.DisplayValues[dimension]; ok {
		if entry, ok := entries[value].(map[string]any); ok {
			for _, key := range []string{"value", "text", "label"} {
				if text, ok := entry[key].(string); ok {
					if cleaned := cleanText(text); cleaned != "" {
						return cleaned
					}
				}
			}
		}
	}
	return formatRawDimension(value)
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

var (
	tagPattern       = regexp.MustCompile(`(?s)<[^>]*>`)
	footnotePattern  = regexp.MustCompile(`(?s)<as-footnote.*?</as-footnote>`)
	marketingPattern = regexp.MustCompile(`(?s)<div[^>]*form-label-small[^>]*>.*?</div>`)
)

func cleanText(s string) string {
	s = marketingPattern.ReplaceAllString(s, "")
	s = footnotePattern.ReplaceAllString(s, "")
	s = tagPattern.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\u00a0", " ")
	return strings.Join(strings.Fields(s), " ")
}

func formatRawDimension(value string) string {
	value = strings.ReplaceAll(value, "_", ".")
	lower := strings.ToLower(value)
	switch {
	case strings.HasSuffix(lower, "inch"):
		return value[:len(value)-4] + " 英寸"
	case strings.HasSuffix(lower, "gb"), strings.HasSuffix(lower, "tb"):
		return strings.ToUpper(value)
	default:
		return value
	}
}

// extractProductSelection returns the JSON object assigned to
// window.PRODUCT_SELECTION_BOOTSTRAP.productSelectionData.
func extractProductSelection(data []byte) (*productSelectionData, error) {
	bootstrap := bytes.Index(data, []byte("PRODUCT_SELECTION_BOOTSTRAP"))
	if bootstrap < 0 {
		return nil, fmt.Errorf("buy-flow bootstrap not found")
	}
	rel := bytes.Index(data[bootstrap:], []byte("productSelectionData:"))
	if rel < 0 {
		return nil, fmt.Errorf("productSelectionData not found")
	}
	index := bootstrap + rel

	start := bytes.IndexByte(data[index:], '{')
	if start < 0 {
		return nil, fmt.Errorf("productSelectionData object not found")
	}
	start += index

	end, err := jsonObjectEnd(data, start)
	if err != nil {
		return nil, err
	}

	var selection productSelectionData
	if err := json.Unmarshal(data[start:end], &selection); err != nil {
		return nil, fmt.Errorf("decode productSelectionData: %w", err)
	}
	return &selection, nil
}

// jsonObjectEnd finds the closing brace of a JSON object, ignoring braces
// inside strings.
func jsonObjectEnd(data []byte, start int) (int, error) {
	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(data); i++ {
		ch := data[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case ch == '\\':
				escaped = true
			case ch == '"':
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated productSelectionData object")
}

var titlePattern = regexp.MustCompile(`(?s)<title>(.*?)</title>`)

func pageTitle(data []byte) string {
	m := titlePattern.FindSubmatch(data)
	if len(m) < 2 {
		return ""
	}
	title := cleanText(string(m[1]))
	title = strings.TrimPrefix(title, "购买 ")
	title = strings.TrimPrefix(title, "Buy ")
	title = strings.TrimPrefix(title, "选购 ")
	if idx := strings.Index(title, " - Apple"); idx >= 0 {
		title = title[:idx]
	}
	return strings.TrimSpace(title)
}

package apple

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const maxPartsPerRequest = 10

// Availability is the result of checking pickup availability for one store.
type Availability struct {
	Store     string
	Parts     []PartAvailability
	Available bool
}

// PartAvailability describes one part number at the requested store.
type PartAvailability struct {
	PartNumber          string
	Available           bool
	AvailableStores     int
	AvailableAtAnyStore bool
	EligibleStores      []string
}

type flexString string

func (s *flexString) UnmarshalJSON(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*s = flexString(fmt.Sprint(value))
	return nil
}

type availabilityResponse struct {
	Head struct {
		Status flexString `json:"status"`
	} `json:"head"`
	Body struct {
		PickupEnabled bool                  `json:"pickupEnabled"`
		Content       []availabilityContent `json:"content"`
	} `json:"body"`
}

type availabilityContent struct {
	PartNumber               string `json:"partNumber"`
	PartAvailableStoresCount int    `json:"partAvailableStoresCount"`
	AvailableAtAnyStore      bool   `json:"availableAtAnyStore"`
	EligibleStores           string `json:"eligibleStores"`
	StoreID                  string `json:"storeId"`
	PickupMessage            *struct {
		StoreID      string                     `json:"storeId"`
		MessageTypes map[string]messageTypeInfo `json:"messageTypes"`
	} `json:"pickupMessage"`
	Pickup *struct {
		Stores []struct {
			StoreID           string                               `json:"storeId"`
			PartsAvailability map[string]partAvailabilityByMessage `json:"partsAvailability"`
		} `json:"stores"`
	} `json:"pickup"`
}

type messageTypeInfo struct {
	StoreSelectionEnabled bool   `json:"storeSelectionEnabled"`
	PickupQuote           string `json:"pickupQuote"`
}

type partAvailabilityByMessage struct {
	MessageTypes map[string]messageTypeInfo `json:"messageTypes"`
}

// CheckPickup reports whether any of parts can be picked up at store.
//
// It uses Apple's current shop-by-availability endpoint. The endpoint returns
// the list of stores with stock ("eligibleStores"), so membership of the
// requested store is enough to decide. Older/alternate response shapes that
// embed a per-store pickupMessage or partsAvailability map are also handled.
func (c *Client) CheckPickup(ctx context.Context, store string, parts []string) (*Availability, error) {
	store = strings.TrimSpace(store)
	parts = normalizeParts(parts)
	if store == "" {
		return nil, fmt.Errorf("store code is empty")
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("no part numbers to check")
	}

	result := &Availability{Store: store}
	seen := make(map[string]bool)

	for start := 0; start < len(parts); start += maxPartsPerRequest {
		end := start + maxPartsPerRequest
		if end > len(parts) {
			end = len(parts)
		}

		query := url.Values{}
		query.Set("fae", "true")
		for i, part := range parts[start:end] {
			query.Set(fmt.Sprintf("parts.%d", i), part)
		}

		var resp availabilityResponse
		path := withQuery("/shop/sba/availability-message", query)
		if err := c.getJSON(ctx, path, &resp); err != nil {
			return nil, err
		}
		if resp.Head.Status != "" && string(resp.Head.Status) != "200" {
			return nil, fmt.Errorf("availability endpoint returned status %s", resp.Head.Status)
		}
		if len(resp.Body.Content) == 0 {
			return nil, fmt.Errorf("availability endpoint returned no content")
		}

		for _, item := range resp.Body.Content {
			part := strings.TrimSpace(item.PartNumber)
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true

			eligible := splitStores(item.EligibleStores)
			available := containsStore(eligible, store)
			if !available {
				available = pickupMessageAvailable(item, store)
			}

			result.Parts = append(result.Parts, PartAvailability{
				PartNumber:          part,
				Available:           available,
				AvailableStores:     item.PartAvailableStoresCount,
				AvailableAtAnyStore: item.AvailableAtAnyStore,
				EligibleStores:      eligible,
			})
			if available {
				result.Available = true
			}
		}
	}

	if len(result.Parts) == 0 {
		return nil, fmt.Errorf("availability endpoint returned no matching parts")
	}

	var missing []string
	for _, part := range parts {
		if !seen[part] {
			missing = append(missing, part)
		}
	}
	if len(missing) > 0 {
		c.Logger.Warn("部分商品未返回库存信息，请检查 SKU 是否正确", "parts", strings.Join(missing, ","))
	}
	return result, nil
}

func normalizeParts(parts []string) []string {
	out := make([]string, 0, len(parts))
	seen := make(map[string]bool)
	for _, part := range parts {
		part = strings.ToUpper(strings.TrimSpace(part))
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return out
}

func splitStores(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	fields := strings.Split(value, ",")
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if store := strings.TrimSpace(field); store != "" {
			out = append(out, store)
		}
	}
	return out
}

func containsStore(stores []string, want string) bool {
	for _, store := range stores {
		if store == want {
			return true
		}
	}
	return false
}

func pickupMessageAvailable(item availabilityContent, store string) bool {
	if item.PickupMessage != nil && (item.PickupMessage.StoreID == "" || item.PickupMessage.StoreID == store) {
		for _, info := range item.PickupMessage.MessageTypes {
			if info.StoreSelectionEnabled {
				return true
			}
		}
	}

	if item.Pickup != nil {
		for _, s := range item.Pickup.Stores {
			if s.StoreID != store {
				continue
			}
			for _, info := range s.PartsAvailability {
				for _, mt := range info.MessageTypes {
					if mt.StoreSelectionEnabled {
						return true
					}
				}
			}
		}
	}
	return false
}

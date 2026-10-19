package apple

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"encoding/json"
)

// storeSearchByLocaleHash is the persisted-query id used by apple.com.cn's
// retail pages for "StoreSearchByLocale". Persisted queries are required by
// the GraphQL endpoint; see the fallback below if Apple rotates the hash.
const storeSearchByLocaleHash = "d95caa379372fe166f35bb89fe8e018e21781c66ee815ee3f68105f36ff4b1d8"

// Stores returns a map of Apple Store code (e.g. R389) to a human readable
// name (e.g. "上海 - 浦东").
func (c *Client) Stores(ctx context.Context) (map[string]string, error) {
	stores, err := c.storesFromGraphQL(ctx)
	if err == nil && len(stores) > 0 {
		return stores, nil
	}
	if err != nil {
		c.Logger.Debug("GraphQL store list failed, falling back to retail page", "error", err)
	}

	stores, fallbackErr := c.storesFromRetailPage(ctx)
	if fallbackErr != nil {
		if err != nil {
			return nil, fmt.Errorf("store list unavailable: %v (fallback: %w)", err, fallbackErr)
		}
		return nil, fallbackErr
	}
	if len(stores) == 0 {
		return nil, fmt.Errorf("store list is empty")
	}
	return stores, nil
}

type graphQLRequest struct {
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
	Extensions    struct {
		PersistedQuery struct {
			Version    int    `json:"version"`
			SHA256Hash string `json:"sha256Hash"`
		} `json:"persistedQuery"`
	} `json:"extensions"`
}

type graphQLResponse struct {
	Data struct {
		RmdLocale struct {
			Stores []struct {
				StoreNumber string `json:"storeNumber"`
				StoreName   string `json:"storeName"`
				Address     struct {
					City  string `json:"city"`
					State struct {
						Name string `json:"name"`
					} `json:"state"`
				} `json:"_address"`
			} `json:"stores"`
		} `json:"rmdLocale"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (c *Client) storesFromGraphQL(ctx context.Context) (map[string]string, error) {
	req := graphQLRequest{
		OperationName: "StoreSearchByLocale",
		Variables:     map[string]any{"localeId": "zh_CN"},
	}
	req.Extensions.PersistedQuery.Version = 1
	req.Extensions.PersistedQuery.SHA256Hash = storeSearchByLocaleHash

	var resp graphQLResponse
	if err := c.postJSON(ctx, "/api-www/graphql", req, &resp); err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("graphql: %s", resp.Errors[0].Message)
	}

	result := make(map[string]string, len(resp.Data.RmdLocale.Stores))
	for _, s := range resp.Data.RmdLocale.Stores {
		name := strings.TrimSpace(s.StoreName)
		if name == "" {
			continue
		}
		region := strings.TrimSpace(s.Address.State.Name)
		if region == "" {
			region = strings.TrimSpace(s.Address.City)
		}
		if region != "" {
			name = region + " - " + name
		}
		result[strings.TrimSpace(s.StoreNumber)] = name
	}
	return result, nil
}

var nextDataPattern = regexp.MustCompile(`(?s)<script id="__NEXT_DATA__" type="application/json">(.*?)</script>`)

type retailLocale struct {
	Locale       string       `json:"locale"`
	CalledLocale string       `json:"calledLocale"`
	State        []retailArea `json:"state"`
	States       []retailArea `json:"states"`
}

type retailStoreList struct {
	Props struct {
		PageProps struct {
			StoreList []retailLocale `json:"storeList"`
		} `json:"pageProps"`
	} `json:"props"`
}

type retailArea struct {
	Name  string `json:"name"`
	Store []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Address struct {
			City      string `json:"city"`
			StateName string `json:"stateName"`
		} `json:"address"`
	} `json:"store"`
}

func (c *Client) storesFromRetailPage(ctx context.Context) (map[string]string, error) {
	data, err := c.get(ctx, "/retail/storelist")
	if err != nil {
		return nil, err
	}

	m := nextDataPattern.FindSubmatch(data)
	if len(m) < 2 {
		return nil, fmt.Errorf("retail store list page has no __NEXT_DATA__ payload")
	}

	var page retailStoreList
	if err := json.Unmarshal(m[1], &page); err != nil {
		return nil, fmt.Errorf("decode retail store list: %w", err)
	}

	var chosen *retailLocale
	for i := range page.Props.PageProps.StoreList {
		entry := &page.Props.PageProps.StoreList[i]
		if entry.Locale == "zh_CN" || entry.CalledLocale == "zh_CN" {
			chosen = entry
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("zh_CN store list not present on retail page")
	}

	areas := chosen.State
	if len(areas) == 0 {
		areas = chosen.States
	}

	result := make(map[string]string)
	for _, area := range areas {
		for _, s := range area.Store {
			region := strings.TrimSpace(s.Address.StateName)
			if region == "" {
				region = strings.TrimSpace(area.Name)
			}
			name := strings.TrimSpace(s.Name)
			if region != "" {
				name = region + " - " + name
			}
			if code := strings.TrimSpace(s.ID); code != "" && name != "" {
				result[code] = name
			}
		}
	}
	return result, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var itemIDRe = regexp.MustCompile(`(?:/product/(\d+)/(\d+)|[.-]i\.(\d+)\.(\d+)|/(\d+)/(\d+))`)

type shopeeClient struct {
	http    *http.Client
	cookies string
}

type shopeeItem struct {
	Name     string
	PriceMin int64
	PriceMax int64
	ShopID   string
	ItemID   string
}

func parseShopItemIDs(u string) (shopID, itemID string) {
	m := itemIDRe.FindStringSubmatch(u)
	if len(m) == 0 {
		return "", ""
	}
	switch {
	case m[1] != "":
		return m[1], m[2]
	case m[3] != "":
		return m[3], m[4]
	case m[5] != "":
		return m[5], m[6]
	default:
		return "", ""
	}
}

func newShopeeClient(cookieHeader string) *shopeeClient {
	return &shopeeClient{
		http: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 12 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
		cookies: cookieHeader,
	}
}

func (c *shopeeClient) resolveAffiliateLink(link string) (shopID, itemID, finalURL string, err error) {
	req, err := http.NewRequest(http.MethodGet, strings.TrimSpace(link), nil)
	if err != nil {
		return "", "", "", err
	}
	setBrowserHeaders(req)
	applyCookieHeader(req, c.cookies)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	finalURL = resp.Request.URL.String()
	shopID, itemID = parseShopItemIDs(finalURL)
	if shopID == "" || itemID == "" {
		return "", "", finalURL, fmt.Errorf("could not parse shop/item from %q", finalURL)
	}
	return shopID, itemID, finalURL, nil
}

func (c *shopeeClient) fetchItem(shopID, itemID string) (shopeeItem, error) {
	var zero shopeeItem
	endpoints := []string{
		fmt.Sprintf("https://shopee.vn/api/v4/item/get?shopid=%s&itemid=%s", shopID, itemID),
		fmt.Sprintf("https://shopee.vn/api/v4/pdp/get_pc?shop_id=%s&item_id=%s", shopID, itemID),
	}
	var lastErr error
	for _, u := range endpoints {
		item, err := c.fetchItemURL(u, shopID, itemID)
		if err == nil {
			return item, nil
		}
		lastErr = err
	}
	return zero, lastErr
}

func (c *shopeeClient) fetchItemURL(apiURL, shopID, itemID string) (shopeeItem, error) {
	var zero shopeeItem
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return zero, err
	}
	setBrowserHeaders(req)
	req.Header.Set("Referer", fmt.Sprintf("https://shopee.vn/product/%s/%s", shopID, itemID))
	req.Header.Set("X-API-SOURCE", "pc")
	req.Header.Set("X-Shopee-Language", "vi")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	applyCookieHeader(req, c.cookies)

	resp, err := c.http.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode >= 400 {
		return zero, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 160))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return zero, err
	}
	if errCode, _ := raw["error"].(float64); errCode != 0 {
		return zero, fmt.Errorf("api error %v: %s", raw["error"], truncate(string(body), 160))
	}

	name, pMin, pMax := extractItemFields(raw)
	if strings.TrimSpace(name) == "" {
		return zero, fmt.Errorf("empty item name in response")
	}
	if pMin == 0 && pMax == 0 {
		return zero, fmt.Errorf("empty price in response")
	}
	if pMax == 0 {
		pMax = pMin
	}
	if pMin == 0 {
		pMin = pMax
	}
	return shopeeItem{
		Name:     strings.TrimSpace(name),
		PriceMin: pMin,
		PriceMax: pMax,
		ShopID:   shopID,
		ItemID:   itemID,
	}, nil
}

func extractItemFields(raw map[string]any) (name string, priceMin, priceMax int64) {
	data, _ := raw["data"].(map[string]any)
	if data == nil {
		return "", 0, 0
	}
	item, _ := data["item"].(map[string]any)
	if item == nil {
		item, _ = data["item_info"].(map[string]any)
	}
	if item == nil {
		item = data
	}
	name, _ = item["name"].(string)
	if name == "" {
		name, _ = item["title"].(string)
	}
	priceMin = int64(num(item["price_min"]))
	priceMax = int64(num(item["price_max"]))
	if priceMin == 0 {
		priceMin = int64(num(item["price"]))
	}
	if priceMax == 0 {
		priceMax = priceMin
	}
	return name, priceMin, priceMax
}

func num(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int64:
		return float64(t)
	case int:
		return float64(t)
	default:
		return 0
	}
}

func setBrowserHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9,en;q=0.8")
	req.Header.Set("Accept", "application/json, text/plain, */*")
}

func formatPriceRange(min, max int64) string {
	toK := func(v int64) int64 {
		if v <= 0 {
			return 0
		}
		// Shopee VN API: 46500000000 = 465k (2024+), legacy 39500000 = 395k.
		if v >= 100000000 {
			return (v + 50000000) / 100000000
		}
		if v >= 100000 {
			return (v + 50000) / 100000
		}
		if v >= 1000 {
			return (v + 500) / 1000
		}
		return v
	}
	a, b := toK(min), toK(max)
	if a == b || b == 0 {
		return fmt.Sprintf("%dk", a)
	}
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%dk-%dk", a, b)
}

func cleanListingTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " | Shopee Việt Nam")
	s = strings.TrimSuffix(s, " | Shopee Vietnam")
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	if len([]rune(s)) > 120 {
		r := []rune(s)
		s = string(r[:120])
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

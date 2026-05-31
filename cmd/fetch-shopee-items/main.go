// One-off helper: resolve s.shopee.vn links and fetch item name/price via Shopee API.
// Usage: go run ./cmd/fetch-shopee-items
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"strings"
	"time"
)

var links = []string{
	"https://s.shopee.vn/70HPIj8Km4",
	"https://s.shopee.vn/2qRqPmwzpY",
	"https://s.shopee.vn/6AiINzImYf",
	"https://s.shopee.vn/1BJcMMK4au",
	"https://s.shopee.vn/7pqWIkqv8j",
	"https://s.shopee.vn/20sjM9NQkT",
	"https://s.shopee.vn/9KfK5f8jH1",
	"https://s.shopee.vn/9UykI17ujY",
	"https://s.shopee.vn/5VSbWhy8zA",
	"https://s.shopee.vn/809wVLfzSU",
	"https://s.shopee.vn/110CAVZWKG",
	"https://s.shopee.vn/4Va4KzPJbs",
	"https://s.shopee.vn/qglyJ74BF",
	"https://s.shopee.vn/1Ld2dpmuQ4",
	"https://s.shopee.vn/6AiIKBTlMf",
	"https://s.shopee.vn/7VDg438uB9",
	"https://s.shopee.vn/BR5BEJ1xB",
}

var idRe = regexp.MustCompile(`/(\d+)/(\d+)(?:\?|$)`)

func main() {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Timeout: 25 * time.Second,
		Jar:     jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	// Warm cookies.
	_, _ = get(client, "https://shopee.vn/")

	for _, link := range links {
		final, err := resolve(client, link)
		if err != nil {
			fmt.Printf("ERR resolve %s: %v\n", link, err)
			continue
		}
		m := idRe.FindStringSubmatch(final)
		if len(m) != 3 {
			fmt.Printf("ERR ids %s -> %s\n", link, final)
			continue
		}
		shopID, itemID := m[1], m[2]
		name, pMin, pMax, err := fetchItem(client, shopID, itemID)
		if err != nil {
			fmt.Printf("ERR item %s shop=%s item=%s: %v\n", link, shopID, itemID, err)
			continue
		}
		fmt.Printf("OK|%s|%s|%s|%s|%s\n", link, shopID, itemID, name, formatPrice(pMin, pMax))
	}
}

func resolve(client *http.Client, u string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	setHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.Request.URL.String(), nil
}

func fetchItem(client *http.Client, shopID, itemID string) (name string, pMin, pMax int64, err error) {
	u := fmt.Sprintf("https://shopee.vn/api/v4/item/get?shopid=%s&itemid=%s", shopID, itemID)
	body, err := get(client, u)
	if err != nil {
		return "", 0, 0, err
	}
	var raw struct {
		Error  int    `json:"error"`
		Data   *struct {
			Name     string `json:"name"`
			Price    int64  `json:"price"`
			PriceMin int64  `json:"price_min"`
			PriceMax int64  `json:"price_max"`
		} `json:"data"`
		Message string `json:"error_msg"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return "", 0, 0, err
	}
	if raw.Error != 0 || raw.Data == nil {
		return "", 0, 0, fmt.Errorf("api error=%d msg=%s body=%s", raw.Error, raw.Message, truncate(string(body), 200))
	}
	pMin = raw.Data.PriceMin
	pMax = raw.Data.PriceMax
	if pMin == 0 {
		pMin = raw.Data.Price
	}
	if pMax == 0 {
		pMax = raw.Data.Price
	}
	return raw.Data.Name, pMin, pMax, nil
}

func get(client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	setHeaders(req)
	if strings.Contains(u, "/api/") {
		req.Header.Set("Referer", "https://shopee.vn/")
		req.Header.Set("X-API-SOURCE", "pc")
		req.Header.Set("X-Shopee-Language", "vi")
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(b), 120))
	}
	return b, nil
}

func setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "vi-VN,vi;q=0.9,en;q=0.8")
}

func formatPrice(pMin, pMax int64) string {
	toK := func(v int64) int64 {
		if v >= 100000 {
			return v / 100000
		}
		return v
	}
	a, b := toK(pMin), toK(pMax)
	if a == b || b == 0 {
		return fmt.Sprintf("%dk", a)
	}
	if a > b {
		a, b = b, a
	}
	return fmt.Sprintf("%dk-%dk", a, b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

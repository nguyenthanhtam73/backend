// sync-affiliate-catalog pulls live Shopee listing title + price for affiliate_catalog.json
// using cookies exported from your logged-in browser (Shopee blocks datacenter bots).
//
// Usage:
//
//	1. Log in to https://shopee.vn in Chrome
//	2. DevTools → Application → Cookies → shopee.vn → copy as header string
//	   OR export JSON via Cookie-Editor extension
//	3. Save to backend/shopee-cookies.txt (gitignored)
//	4. go run ./cmd/sync-affiliate-catalog --cookies shopee-cookies.txt
//
// Options:
//
//	--dry-run          print changes, do not write JSON
//	--update-names     overwrite product_name from Shopee listing title
//	--catalog path     default: internal/service/ai/affiliate_catalog.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type catalogEntry struct {
	ID            string   `json:"id"`
	ProductName   string   `json:"product_name"`
	Brand         string   `json:"brand"`
	Category      string   `json:"category"`
	SkinTypes     []string `json:"skin_types"`
	Concerns      []string `json:"concerns"`
	PriceRange    string   `json:"price_range"`
	AffiliateLink string   `json:"affiliate_link"`
}

func main() {
	cookiesPath := flag.String("cookies", envOr("SHOPEE_COOKIES_FILE", "shopee-cookies.txt"), "path to cookie file (or set SHOPEE_COOKIES_FILE)")
	catalogPath := flag.String("catalog", "internal/service/ai/affiliate_catalog.json", "affiliate catalog JSON to update")
	dryRun := flag.Bool("dry-run", false, "print updates without writing catalog")
	updateNames := flag.Bool("update-names", false, "overwrite product_name with Shopee listing title")
	flag.Parse()

	cookieHeader, err := loadCookieHeader(*cookiesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cookie error: %v\n\nCreate %s — see cmd/sync-affiliate-catalog/main.go header for formats.\n", err, *cookiesPath)
		os.Exit(1)
	}
	if !strings.Contains(strings.ToLower(cookieHeader), "spc_") {
		fmt.Fprintln(os.Stderr, "warn: cookie header has no SPC_* keys — export cookies while logged in to shopee.vn")
	}

	absCatalog, err := filepath.Abs(*catalogPath)
	if err != nil {
		fatal(err)
	}
	raw, err := os.ReadFile(absCatalog)
	if err != nil {
		fatal(err)
	}
	var rows []catalogEntry
	if err := json.Unmarshal(raw, &rows); err != nil {
		fatal(err)
	}

	client := newShopeeClient(cookieHeader)
	ok, fail := 0, 0

	for i := range rows {
		e := &rows[i]
		link := strings.TrimSpace(e.AffiliateLink)
		if link == "" {
			fmt.Printf("SKIP %-28s no affiliate_link\n", e.ID)
			fail++
			continue
		}

		shopID, itemID, finalURL, err := client.resolveAffiliateLink(link)
		if err != nil {
			fmt.Printf("FAIL %-28s resolve: %v\n", e.ID, err)
			fail++
			continue
		}

		item, err := client.fetchItem(shopID, itemID)
		if err != nil {
			fmt.Printf("FAIL %-28s fetch: %v (shop=%s item=%s)\n", e.ID, err, shopID, itemID)
			fail++
			continue
		}

		newPrice := formatPriceRange(item.PriceMin, item.PriceMax)
		newName := cleanListingTitle(item.Name)
		fmt.Printf("OK   %-28s %s → %s", e.ID, e.PriceRange, newPrice)
		if e.ProductName != newName {
			fmt.Printf(" | listing: %q", newName)
		}
		fmt.Println()

		e.PriceRange = newPrice
		if *updateNames && newName != "" {
			e.ProductName = newName
		}
		ok++

		_ = finalURL
		time.Sleep(400 * time.Millisecond)
	}

	fmt.Printf("\nDone: %d updated, %d failed (of %d)\n", ok, fail, len(rows))
	if fail > 0 && ok == 0 {
		os.Exit(2)
	}
	if *dryRun {
		fmt.Println("dry-run: catalog not written")
		return
	}
	if ok == 0 {
		return
	}

	out, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(absCatalog, out, 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("Wrote %s\n", absCatalog)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

package adminmetrics

import "github.com/dadiary/backend/internal/service/ai"

func init() {
	lookupCatalogSKU = func(link, fallbackName, fallbackBrand string) (id, name, brand string, ok bool) {
		id, name, brand, ok = ai.LookupCatalogByLink(link)
		if ok {
			return id, name, brand, true
		}
		return "", fallbackName, fallbackBrand, false
	}
}

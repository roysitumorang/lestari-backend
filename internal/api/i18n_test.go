package api

import (
	"testing"

	"github.com/roysitumorang/lestari-backend/i18n"
)

// Fixed catalogues are sent in the language of the request; every text needs a Bahasa Indonesia entry.
func TestCataloguesTranslated(t *testing.T) {
	var texts []string
	for _, m := range missions {
		texts = append(texts, m.Title, m.Description)
	}
	for _, i := range integrations {
		texts = append(texts, i.Name, i.Description)
	}
	for _, p := range loanProviders {
		texts = append(texts, p["description"].(string), p["interest_info"].(string))
	}
	for _, p := range pluginCatalog {
		texts = append(texts, p.Name, p.Description)
		texts = append(texts, p.Features...)
	}
	// codes shown through the dictionary (statuses, payment methods, kinds)
	texts = append(texts, statusCodes...)
	for _, s := range texts {
		if !i18n.Has(s) {
			t.Errorf("no Bahasa Indonesia entry for %q", s)
		}
	}
}

// statusCodes are values the API stores and documents/messages print through the dictionary.
var statusCodes = []string{
	"pending", "paid", "unpaid", "partially_paid", "completed", "cancelled", "expired", "failed", "success",
	"accepted", "rejected", "received", "in_transit", "approved", "processing", "shipped", "delivered", "new",
	"cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "virtual_account", "manual_transfer",
	"convenience_store", "coin", "payable", "receivable", "sale", "purchase",
}

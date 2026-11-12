package models

import (
	"testing"

	"github.com/roysitumorang/lestari-backend/i18n"
)

// statusCodes are values the API stores and documents and messages print through the dictionary.
var statusCodes = []string{
	"pending", "paid", "unpaid", "partially_paid", "completed", "cancelled", "expired", "failed", "success",
	"accepted", "rejected", "received", "in_transit", "approved", "processing", "shipped", "delivered", "new",
	"cash", "debit", "credit_card", "qris", "e_wallet", "bank_transfer", "virtual_account", "manual_transfer",
	"convenience_store", "coin", "payable", "receivable", "sale", "purchase",
}

// TestCataloguesTranslated checks that the texts of the built-in catalogues (missions, integrations, lending
// partners, plugins) and the codes printed on documents have a Bahasa Indonesia entry.
func TestCataloguesTranslated(t *testing.T) {
	texts := append([]string{}, statusCodes...)
	texts = append(texts, OrderStatuses...)
	texts = append(texts, SalePaymentMethods...)
	for _, item := range Missions {
		texts = append(texts, item.Title, item.Description)
	}
	for _, item := range Integrations {
		texts = append(texts, item.Name, item.Description)
	}
	for _, item := range LoanProviders {
		texts = append(texts, item.Description, item.InterestInfo)
	}
	for _, item := range PluginCatalog {
		texts = append(texts, item.Name, item.Description)
		texts = append(texts, item.Features...)
	}
	for _, text := range texts {
		if !i18n.Has(text) {
			t.Errorf("no Bahasa Indonesia entry for %q", text)
		}
	}
}

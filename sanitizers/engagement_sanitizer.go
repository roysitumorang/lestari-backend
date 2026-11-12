package sanitizers

import (
	"log/slog"
	"slices"
	"strings"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// IntegrationActivation reads the body of an activation: the terms must be accepted; settings are optional.
func IntegrationActivation(c fiber.Ctx) (json.RawMessage, error) {
	ctxt := "EngagementSanitizer-IntegrationActivation"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	if !helper.Bool(b, "accept_terms") {
		return nil, helper.Invalid("accept_terms", "Accept the terms of service to continue.")
	}
	if settings, ok := b["settings"].(map[string]any); ok && len(settings) > 0 {
		return json.Marshal(settings)
	}
	return nil, nil
}

// LoanApplication reads the body of a loan application and checks it against the offer of the lending partner.
func LoanApplication(c fiber.Ctx) (*models.LoanApplication, error) {
	request, err := Decode[models.LoanApplicationRequest](c)
	if err != nil {
		return nil, err
	}
	v := &helper.Validation{}
	if provider := models.LoanProviderByCode(request.ProviderCode); provider == nil {
		v.Add("provider_code", "Choose a lending partner.")
	} else {
		if request.Amount < provider.MinAmount || request.Amount > provider.MaxAmount {
			v.Add("amount", "The amount is outside this partner's range.")
		}
		if !slices.Contains(provider.TenorsMonths, request.TenorMonths) {
			v.Add("tenor_months", "Choose one of the offered tenors.")
		}
	}
	purpose := strings.TrimSpace(request.Purpose)
	if purpose == "" {
		v.Add("purpose", "Required")
	}
	if !request.Consent {
		v.Add("consent", "Consent is required to share your data with the partner.")
	}
	return &models.LoanApplication{
		ProviderCode: request.ProviderCode,
		Amount:       request.Amount,
		TenorMonths:  request.TenorMonths,
		Purpose:      purpose,
	}, v.Err()
}

// StoreRating reads the body of a store rating: one to five stars.
func StoreRating(c fiber.Ctx) (*models.StoreRating, error) {
	ctxt := "EngagementSanitizer-StoreRating"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	rating, _ := helper.Int(b, "rating")
	if rating < 1 || rating > 5 {
		return nil, helper.Invalid("rating", "Give 1 to 5 stars.")
	}
	response := &models.StoreRating{
		Rating:       rating,
		CustomerName: helper.Str(b, "customer_name"),
		Tags:         []string{},
	}
	if comment := helper.Str(b, "comment"); comment != "" {
		response.Comment = &comment
	}
	if saleID, ok := helper.UUID(b, "sale_id"); ok {
		if id, err := uuid.Parse(saleID); err == nil {
			response.SaleID = &id
		}
	}
	if tags, ok := b["tags"].([]any); ok {
		for _, tag := range tags {
			if text, ok := tag.(string); ok {
				response.Tags = append(response.Tags, text)
			}
		}
	}
	return response, nil
}

package sanitizers

import (
	"log/slog"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
)

// password reads a base64 password; nil means "not sent".
func (b body) password(v *helper.Validation, n nuller, key string) *string {
	encoded := b.text(v, n, key)
	if encoded == nil {
		return nil
	}
	plain, ok := helper.DecodeBase64Password(*encoded)
	if !ok {
		v.Add(key, "Send the password base64-encoded.")
		return nil
	}
	return &plain
}

// FindBranches reads the list query: q, sort, order, page, per_page.
func FindBranches(c fiber.Ctx) *models.BranchFilter {
	response := models.NewBranchFilter()
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		response.WithKeyword(keyword)
	}
	limit, page := paging(c)
	if sortBy, ok := sorting(c, models.MapBranchSortings); ok {
		response.WithSortings(sortBy)
	}
	return response.
		WithLimit(limit).
		WithPage(page)
}

// ValidateBranch reads a branch body. On create name and email are required; on update only what was sent is
// checked.
func ValidateBranch(c fiber.Ctx, create bool) (*models.BranchRequest, error) {
	ctxt := "BranchSanitizer-ValidateBranch"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.BranchRequest{}
	response.Name = b.text(v, response, "name")
	response.Code = b.text(v, response, "code")
	response.ManagerName = b.text(v, response, "manager_name")
	response.Phone = b.text(v, response, "phone")
	response.Email = b.text(v, response, "email")
	response.Password = b.password(v, response, "password")
	response.Address = b.text(v, response, "address")
	if response.Email != nil {
		*response.Email = strings.ToLower(*response.Email)
	}
	required(v, "name", response.Name == nil && (create || response.IsNull("name")))
	required(v, "email", response.Email == nil && (create || response.IsNull("email")))
	return response, v.Err()
}

// BranchRenewal reads the body of a branch renewal and returns the months of its period.
func BranchRenewal(c fiber.Ctx) (*models.BranchRenewalRequest, int, error) {
	ctxt := "BranchSanitizer-BranchRenewal"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, 0, err
	}
	v := &helper.Validation{}
	nulls := &models.Nulls{}
	response := &models.BranchRenewalRequest{
		BranchIDs:     b.idList(v, nulls, "branch_ids"),
		Period:        helper.Str(b, "period"),
		PaymentMethod: helper.Str(b, "payment_method"),
		PromoCode:     helper.Str(b, "promo_code"),
	}
	months, ok := PeriodMonths[response.Period]
	if !ok {
		v.Add("period", "Must be one of: monthly, quarterly, yearly.")
	}
	if len(response.BranchIDs) == 0 && len(v.Fields["branch_ids"]) == 0 {
		v.Add("branch_ids", "Choose at least one branch.")
	}
	if !payable(response.PaymentMethod, true) {
		v.Add("payment_method", "Choose a payment method.")
	}
	return response, months, v.Err()
}

// CopyProducts reads the body of a product copy.
func CopyProducts(c fiber.Ctx) (*models.CopyProductsRequest, error) {
	ctxt := "BranchSanitizer-CopyProducts"
	b, err := parseBody(c)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParseBody")
		return nil, err
	}
	v := &helper.Validation{}
	response := &models.CopyProductsRequest{
		CategoryIDs:  b.idList(v, &models.Nulls{}, "category_ids"),
		IncludeStock: helper.Bool(b, "include_stock"),
	}
	return response, v.Err()
}

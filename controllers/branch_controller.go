package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type branchController struct {
	authService    *middlewares.Service
	branchService  services.BranchService
	billingService services.BillingService
}

func NewBranchController(
	authService *middlewares.Service,
	branchService services.BranchService,
	billingService services.BillingService,
) *branchController {
	return &branchController{
		authService:    authService,
		branchService:  branchService,
		billingService: billingService,
	}
}

// Mount registers the routes of /branches and the branch subscriptions under /billing.
func (q *branchController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	branches := private.Group("/branches")
	branches.Get("", q.V1Index)
	branches.Post("", q.V1Create)
	branches.Get("/:id<guid>", q.V1Show)
	branches.Put("/:id<guid>", q.V1Update)
	branches.Delete("/:id<guid>", q.V1Delete)
	branches.Post("/:id<guid>/copy-products", q.V1CopyProducts)
	private.Get("/billing/branches", q.V1Billing)
	private.Post("/billing/branch-renewals", q.V1Renew)
}

// branchError turns the branch errors into responses; anything else goes to the error handler as it is.
func branchError(err error) error {
	if errors.Is(err, models.ErrBranchNotFound) {
		return helper.NotFound("Branch")
	}
	return writeError(err)
}

// manager refuses anyone but the owner and administrators.
func manager(c fiber.Ctx, ctxt, refusal string) error {
	if role := helper.P(c).Role; role != models.RoleOwner && role != models.RoleAdministrator {
		err := helper.Forbidden(refusal)
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	return nil
}

// oneBranch selects a branch of the signed-in user's store.
func oneBranch(c fiber.Ctx, ctxt string) (*models.BranchFilter, error) {
	branchID, err := sanitizers.ParamID(c, "id", "Branch")
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return nil, err
	}
	return models.NewBranchFilter().WithStoreIDs(currentStoreID(c)).WithBranchIDs(branchID), nil
}

// V1Index godoc
//
//	@Summary	List branches
//	@Tags		Branches
//	@Param		q			query		string	false	"search in name, code"
//	@Param		sort		query		string	false	"sort field"		Enums(name, code, manager_name, phone, email, address, is_head_office, subscription_expires_at, created_at, updated_at)
//	@Param		order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param		page		query		int		false	"page, from 1"		default(1)
//	@Param		per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.Branch}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/branches [get]
func (q *branchController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Index"
	request := sanitizers.FindBranches(c).WithStoreIDs(currentStoreID(c))
	response, total, err := q.branchService.FindBranches(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranches")
		return err
	}
	return helper.List(c, response, page(request.Limit, request.Page, total))
}

// V1Show godoc
//
//	@Summary	Get one branch
//	@Tags		Branches
//	@Param		id	path		string	true	"branch id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Branch}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/branches/{id} [get]
func (q *branchController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Show"
	filter, err := oneBranch(c, ctxt)
	if err != nil {
		return err
	}
	response, err := q.branchService.FindBranch(ctx, nil, filter)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranch")
		return branchError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary		Create a branch
//	@Description	Only the owner or an administrator. The branch gets a login of its own (email and password), default payment methods, a cash drawer and the PPOB price list; its subscription follows the plan of the store.
//	@Tags			Branches
//	@Param			request	body		models.BranchRequest	true	"branch"
//	@Success		201		{object}	helper.Response{data=models.Branch}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branches [post]
func (q *branchController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Create"
	request, err := sanitizers.ValidateBranch(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateBranch")
		return err
	}
	if err = manager(c, ctxt, "Only the owner or an administrator can manage branches."); err != nil {
		return err
	}
	now := time.Now()
	branch := request.Apply(&models.Branch{StoreID: currentStoreID(c), CreatedAt: now, UpdatedAt: now})
	var response *models.Branch
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.branchService.CreateBranch(ctx, tx, branch); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateBranch")
		}
		return branchError(err)
	}); err != nil {
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a branch
//	@Description	Only the owner or an administrator. Only the attributes present in the body are changed; a new email or password also changes the login of the branch. When no value differs from the stored one nothing is written.
//	@Tags			Branches
//	@Param			id		path		string					true	"branch id (UUID)"
//	@Param			request	body		models.BranchRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Branch}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branches/{id} [put]
func (q *branchController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Update"
	filter, err := oneBranch(c, ctxt)
	if err != nil {
		return err
	}
	request, err := sanitizers.ValidateBranch(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateBranch")
		return err
	}
	if err = manager(c, ctxt, "Only the owner or an administrator can manage branches."); err != nil {
		return err
	}
	branch, err := q.branchService.FindBranch(ctx, nil, filter)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranch")
		return branchError(err)
	}
	var response *models.Branch
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		request.Apply(branch).UpdatedAt = time.Now()
		if response, err = q.branchService.UpdateBranch(ctx, tx, branch); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateBranch")
		}
		return branchError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a branch
//	@Description	Only the owner. The users of the branch are deleted with it; the head office cannot be deleted.
//	@Tags			Branches
//	@Param			id	path	string	true	"branch id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branches/{id} [delete]
func (q *branchController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Delete"
	branchID, err := sanitizers.ParamID(c, "id", "Branch")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	if !helper.P(c).IsOwner() {
		err = helper.Forbidden("Only the owner can delete branches.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrForbidden")
		return err
	}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		err := q.branchService.DeleteBranch(ctx, tx, currentStoreID(c), branchID, time.Now())
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteBranch")
		}
		return branchError(err)
	}); err != nil {
		return err
	}
	return helper.NoContent(c)
}

// V1CopyProducts godoc
//
//	@Summary		Copy head-office products to a branch
//	@Description	Starts a background job that copies the categories and products the branch does not have yet (matched by name and SKU), optionally with their stock. The branch is notified when it is done.
//	@Tags			Branches
//	@Param			id		path		string						true	"branch id (UUID)"
//	@Param			request	body		models.CopyProductsRequest	false	"what to copy"
//	@Success		200		{object}	helper.Response{data=models.JobResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branches/{id}/copy-products [post]
func (q *branchController) V1CopyProducts(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1CopyProducts"
	branchID, err := sanitizers.ParamID(c, "id", "Branch")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.CopyProducts(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCopyProducts")
		return err
	}
	response, err := q.branchService.CopyProducts(ctx, &models.ProductCopy{
		StoreID:      currentStoreID(c),
		HeadOfficeID: currentHeadOfficeID(c),
		BranchID:     branchID,
		CategoryIDs:  request.CategoryIDs,
		IncludeStock: request.IncludeStock,
		At:           time.Now(),
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCopyProducts")
		return branchError(err)
	}
	return helper.OK(c, response)
}

// V1Billing godoc
//
//	@Summary	List branch subscriptions
//	@Tags		Billing
//	@Param		page		query		int	false	"page, from 1"	default(1)
//	@Param		per_page	query		int	false	"rows per page"	default(25)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.BranchBilling}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/billing/branches [get]
func (q *branchController) V1Billing(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Billing"
	limit, pageNo := sanitizers.PageOf(c, models.DefaultLimit)
	response, total, err := q.branchService.FindBranchBillings(ctx, currentStoreID(c), time.Now(), limit, pageNo)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchBillings")
		return err
	}
	return helper.List(c, response, page(limit, pageNo, total))
}

// V1Renew godoc
//
//	@Summary		Renew branch subscriptions
//	@Description	Creates an invoice that extends the subscription of the chosen branches by a month, a quarter or a year (15% off).
//	@Tags			Billing
//	@Param			request	body		models.BranchRenewalRequest	true	"branches, period and payment"
//	@Success		200		{object}	helper.Response{data=models.Invoice}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/billing/branch-renewals [post]
func (q *branchController) V1Renew(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchController-V1Renew"
	request, months, err := sanitizers.BranchRenewal(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBranchRenewal")
		return err
	}
	found, err := q.branchService.CountBranches(ctx, currentStoreID(c), request.BranchIDs)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCountBranches")
		return err
	}
	if found != int64(len(request.BranchIDs)) {
		err = helper.Invalid("branch_ids", "Some branches were not found.")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBranchesNotFound")
		return err
	}
	amount := models.BranchRenewalMonthly * float64(months) * float64(found)
	if months == 12 {
		amount *= 0.85 // yearly discount
	}
	var response *models.Invoice
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response, err = q.billingService.CreateInvoice(ctx, tx, &models.InvoiceOrder{
			StoreID:       currentStoreID(c),
			Kind:          models.InvoiceKindBranchRenewal,
			Description:   helper.T(c, "Renewal of {n} branch(es) for {months} month(s)", "n", found, "months", months),
			PaymentMethod: request.PaymentMethod,
			Amount:        amount,
			PromoCode:     request.PromoCode,
			Meta:          models.InvoiceMeta{BranchIDs: request.BranchIDs, Months: months},
			Lang:          helper.Lang(c),
			At:            time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateInvoice")
		}
		return billingError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

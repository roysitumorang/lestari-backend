package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type branchProductController struct {
	authService          *middlewares.Service
	branchProductService services.BranchProductService
}

func NewBranchProductController(
	authService *middlewares.Service,
	branchProductService services.BranchProductService,
) *branchProductController {
	return &branchProductController{
		authService:          authService,
		branchProductService: branchProductService,
	}
}

// Mount registers the routes of /branch-products; all of them need the business plugin.
func (q *branchProductController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware(), middlewares.RequirePlugin("business"))
	private.Get("", q.V1Index)
	private.Get("/export", q.V1Export)
	private.Post("/sync-prices", q.V1SyncPrices)
	private.Put("/:id<guid>", q.V1Update)
}

// branchProductError turns the branch product errors into responses; anything else goes to the error handler as it is.
func branchProductError(err error) error {
	if errors.Is(err, models.ErrBranchProductNotFound) {
		return helper.NotFound("Branch product")
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List branch products
//	@Description	The products of every branch of the store with the prices each branch sells them at. Needs the business plugin.
//	@Tags			Branch products
//	@Param			q			query		string	false	"search in product_name, sku"
//	@Param			branch_id	query		string	false	"only products of this branch"
//	@Param			category_id	query		string	false	"only products of this category"
//	@Param			sort		query		string	false	"sort field"		Enums(branch_id, branch_name, product_name, sku, category_id, stock, selling_price, purchase_price, is_available, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.BranchProduct}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branch-products [get]
func (q *branchProductController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchProductController-V1Index"
	request := sanitizers.FindBranchProducts(c).
		WithStoreIDs(currentStoreID(c))
	response, total, err := q.branchProductService.FindBranchProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchProducts")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// branchProductExportColumns are the columns of the export.
var branchProductExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "branch_id", Label: "branch_id"},
	{Key: "branch_name", Label: "branch_name"},
	{Key: "product_name", Label: "product_name"},
	{Key: "sku", Label: "sku"},
	{Key: "category_id", Label: "category_id"},
	{Key: "stock", Label: "stock", Format: "number"},
	{Key: "selling_price", Label: "selling_price", Format: "number"},
	{Key: "purchase_price", Label: "purchase_price", Format: "number"},
	{Key: "is_available", Label: "is_available", Format: "boolean"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export branch products
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Branch products
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format		query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q			query		string	false	"search in product_name, sku"
//	@Param			branch_id	query		string	false	"only products of this branch"
//	@Param			category_id	query		string	false	"only products of this category"
//	@Param			sort		query		string	false	"sort field"		Enums(branch_id, branch_name, product_name, sku, category_id, stock, selling_price, purchase_price, is_available, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Success		200			{file}		binary
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branch-products/export [get]
func (q *branchProductController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchProductController-V1Export"
	request := sanitizers.FindBranchProducts(c).
		WithStoreIDs(currentStoreID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.branchProductService.FindBranchProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchProducts")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "branch-products", "Branch products", branchProductExportColumns, rows)
}

// V1Update godoc
//
//	@Summary		Update a branch product
//	@Description	Changes the prices or availability of a product in its branch. Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Branch products
//	@Param			id		path		string						true	"product id (UUID)"
//	@Param			request	body		models.BranchProductRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.BranchProduct}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branch-products/{id} [put]
func (q *branchProductController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchProductController-V1Update"
	branchProductID, err := sanitizers.ParamID(c, "id", "Branch product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateBranchProduct(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateBranchProduct")
		return err
	}
	branchProduct, err := q.branchProductService.FindBranchProduct(
		ctx,
		nil,
		models.NewBranchProductFilter().
			WithStoreIDs(currentStoreID(c)).
			WithBranchProductIDs(branchProductID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindBranchProduct")
		return branchProductError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(branchProduct).UpdatedAt = time.Now()
	response, err := q.branchProductService.UpdateBranchProduct(ctx, tx, branchProduct)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateBranchProduct")
		return branchProductError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1SyncPrices godoc
//
//	@Summary		Copy head-office prices to branches
//	@Description	Sets the selling price of the products of the chosen branches to the price of the matching head-office product (the product it was copied from, or the one with the same SKU). Products that already have that price are left alone.
//	@Tags			Branch products
//	@Param			request	body		models.BranchPriceSyncRequest	true	"branches and, optionally, products"
//	@Success		200		{object}	helper.Response{data=models.UpdatedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		402		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/branch-products/sync-prices [post]
func (q *branchProductController) V1SyncPrices(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "BranchProductController-V1SyncPrices"
	request, err := sanitizers.ValidateBranchPriceSync(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateBranchPriceSync")
		return err
	}
	request.StoreID = currentStoreID(c)
	request.HeadOfficeID = currentHeadOfficeID(c)
	request.At = time.Now()
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	updated, err := q.branchProductService.SyncPrices(ctx, tx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSyncPrices")
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.UpdatedResponse{Updated: updated})
}

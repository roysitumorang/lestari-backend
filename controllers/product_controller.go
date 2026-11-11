package controllers

import (
	"errors"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type productController struct {
	authService    *middlewares.Service
	productService services.ProductService
}

func NewProductController(
	authService *middlewares.Service,
	productService services.ProductService,
) *productController {
	return &productController{
		authService:    authService,
		productService: productService,
	}
}

// Mount registers the routes. The id routes only match UUIDs, so other paths under /products fall through.
func (q *productController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Post("/bulk-delete", q.V1BulkDelete)
	private.Post("/import", q.V1Import)
	private.Get("/import-template", q.V1ImportTemplate)
	private.Get("/export", q.V1Export)
	private.Get("/:id<guid>", q.V1Show)
	private.Put("/:id<guid>", q.V1Update)
	private.Delete("/:id<guid>", q.V1Delete)
	q.mountExtras(private)
}

// productError turns the product errors into responses; anything else goes to the error handler as it is.
func productError(err error) error {
	switch {
	case errors.Is(err, models.ErrProductNotFound):
		return helper.NotFound("Product")
	case errors.Is(err, models.ErrProductConflict):
		return helper.Conflict("A {item} with the same code or name already exists.", "item", i18n.Tr("Product"))
	case errors.Is(err, models.ErrProductInUse):
		return helper.Conflict("This {item} is still used by other records.", "item", i18n.Tr("Product"))
	}
	return writeError(err)
}

// V1Index godoc
//
//	@Summary		List products
//	@Description	Products of the active branch.
//	@Tags			Products
//	@Param			q				query		string	false	"search in name, sku, barcode"
//	@Param			item_type		query		string	false	"only rows with this item type"	Enums(product, service)
//	@Param			category_id		query		string	false	"only rows with this category id"
//	@Param			stock_status	query		string	false	"in_stock, low or out"	Enums(in_stock, low, out)
//	@Param			sort			query		string	false	"sort field"			Enums(image_url, name, item_type, sku, barcode, category_id, unit, selling_price, purchase_price, price_type, tax_id, discount_type, discount_value, wholesale_prices, customer_type_prices, track_stock, stock, minimum_stock, shelf_location, weight_grams, track_imei, units, variants, ingredients, points_earned, points_to_redeem, show_in_online_store, description, online_description, category.name, tax.name, created_at, updated_at)
//	@Param			order			query		string	false	"sort direction"		Enums(asc, desc)	default(asc)
//	@Param			page			query		int		false	"page, from 1"			default(1)
//	@Param			per_page		query		int		false	"rows per page"			default(25)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Product}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products [get]
func (q *productController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Index"
	request := sanitizers.FindProducts(c).
		WithBranchIDs(currentBranchID(c))
	response, total, err := q.productService.FindProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProducts")
		return err
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one product
//	@Tags		Products
//	@Param		id	path		string	true	"product id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.Product}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/products/{id} [get]
func (q *productController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Show"
	productID, err := sanitizers.ParamID(c, "id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.productService.FindProduct(
		ctx,
		nil,
		models.NewProductFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(productID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
		return productError(err)
	}
	return helper.OK(c, response)
}

// V1Create godoc
//
//	@Summary	Create a product
//	@Tags		Products
//	@Param		request	body		models.ProductRequest	true	"product"
//	@Success	201		{object}	helper.Response{data=models.Product}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/products [post]
func (q *productController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Create"
	request, err := sanitizers.ValidateProduct(c, true)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateProduct")
		return err
	}
	now := time.Now()
	product := request.Apply(&models.Product{
		BranchID:  currentBranchID(c),
		Actor:     currentActor(c),
		CreatedAt: now,
		UpdatedAt: now,
	})
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.productService.CreateProduct(ctx, tx, product)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateProduct")
		return productError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response)
}

// V1Update godoc
//
//	@Summary		Update a product
//	@Description	Only the attributes present in the body are changed. When no value differs from the stored one nothing is written and updated_at stays as it was.
//	@Tags			Products
//	@Param			id		path		string					true	"product id (UUID)"
//	@Param			request	body		models.ProductRequest	true	"attributes to change"
//	@Success		200		{object}	helper.Response{data=models.Product}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/{id} [put]
func (q *productController) V1Update(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Update"
	productID, err := sanitizers.ParamID(c, "id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	request, err := sanitizers.ValidateProduct(c, false)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateProduct")
		return err
	}
	product, err := q.productService.FindProduct(
		ctx,
		nil,
		models.NewProductFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(productID),
	)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
		return productError(err)
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	request.Apply(product).UpdatedAt = time.Now()
	response, err := q.productService.UpdateProduct(ctx, tx, product)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrUpdateProduct")
		return productError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response)
}

// V1Delete godoc
//
//	@Summary		Delete a product
//	@Description	A product that other records still use cannot be deleted (409).
//	@Tags			Products
//	@Param			id	path	string	true	"product id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/{id} [delete]
func (q *productController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Delete"
	productID, err := sanitizers.ParamID(c, "id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.productService.DeleteProducts(
		ctx,
		tx,
		models.NewProductFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(productID),
		time.Now(),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteProducts")
		return productError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1BulkDelete godoc
//
//	@Summary		Delete several products
//	@Description	All or nothing: an unknown id (404) or a product still in use (409) deletes none.
//	@Tags			Products
//	@Param			request	body		models.BulkDeleteRequest	true	"ids to delete"
//	@Success		200		{object}	helper.Response{data=models.DeletedResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/bulk-delete [post]
func (q *productController) V1BulkDelete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1BulkDelete"
	productIDs, err := sanitizers.BulkDeleteIDs(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBulkDeleteIDs")
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.productService.DeleteProducts(
		ctx,
		tx,
		models.NewProductFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(productIDs...),
		time.Now(),
	); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteProducts")
		return productError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, models.DeletedResponse{Deleted: len(productIDs)})
}

// productExportColumns are the columns of the export; the headers are the attribute names so the file can be imported again.
var productExportColumns = []export.Column{
	{Key: "id", Label: "id"},
	{Key: "image_url", Label: "image_url"},
	{Key: "name", Label: "name"},
	{Key: "item_type", Label: "item_type"},
	{Key: "sku", Label: "sku"},
	{Key: "barcode", Label: "barcode"},
	{Key: "category_id", Label: "category_id"},
	{Key: "category.name", Label: "category_name"},
	{Key: "unit", Label: "unit"},
	{Key: "selling_price", Label: "selling_price", Format: "number"},
	{Key: "purchase_price", Label: "purchase_price", Format: "number"},
	{Key: "price_type", Label: "price_type"},
	{Key: "tax_id", Label: "tax_id"},
	{Key: "tax.name", Label: "tax_name"},
	{Key: "discount_type", Label: "discount_type"},
	{Key: "discount_value", Label: "discount_value", Format: "number"},
	{Key: "wholesale_prices", Label: "wholesale_prices"},
	{Key: "customer_type_prices", Label: "customer_type_prices"},
	{Key: "track_stock", Label: "track_stock", Format: "boolean"},
	{Key: "stock", Label: "stock", Format: "number"},
	{Key: "minimum_stock", Label: "minimum_stock", Format: "number"},
	{Key: "shelf_location", Label: "shelf_location"},
	{Key: "weight_grams", Label: "weight_grams", Format: "number"},
	{Key: "track_imei", Label: "track_imei", Format: "boolean"},
	{Key: "units", Label: "units"},
	{Key: "variants", Label: "variants"},
	{Key: "ingredients", Label: "ingredients"},
	{Key: "points_earned", Label: "points_earned", Format: "number"},
	{Key: "points_to_redeem", Label: "points_to_redeem", Format: "number"},
	{Key: "show_in_online_store", Label: "show_in_online_store", Format: "boolean"},
	{Key: "description", Label: "description"},
	{Key: "online_description", Label: "online_description"},
	{Key: "created_at", Label: "created_at", Format: "datetime"},
}

// V1Export godoc
//
//	@Summary		Export products
//	@Description	The rows of the list (same filters and sorting, up to 50,000) as a file.
//	@Tags			Products
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			q				query		string	false	"search in name, sku, barcode"
//	@Param			item_type		query		string	false	"only rows with this item type"	Enums(product, service)
//	@Param			category_id		query		string	false	"only rows with this category id"
//	@Param			stock_status	query		string	false	"in_stock, low or out"	Enums(in_stock, low, out)
//	@Param			sort			query		string	false	"sort field"			Enums(image_url, name, item_type, sku, barcode, category_id, unit, selling_price, purchase_price, price_type, tax_id, discount_type, discount_value, wholesale_prices, customer_type_prices, track_stock, stock, minimum_stock, shelf_location, weight_grams, track_imei, units, variants, ingredients, points_earned, points_to_redeem, show_in_online_store, description, online_description, category.name, tax.name, created_at, updated_at)
//	@Param			order			query		string	false	"sort direction"		Enums(asc, desc)	default(asc)
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/export [get]
func (q *productController) V1Export(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Export"
	request := sanitizers.FindProducts(c).
		WithBranchIDs(currentBranchID(c)).
		WithLimit(50000).
		WithPage(1)
	response, _, err := q.productService.FindProducts(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProducts")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	return export.Write(c, "products", "Products", productExportColumns, rows)
}

// V1ImportTemplate godoc
//
//	@Summary		Download the product import template
//	@Description	An .xlsx file with one column per attribute; a * marks the required ones.
//	@Tags			Products
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Success		200	{file}		binary
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/import-template [get]
func (q *productController) V1ImportTemplate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1ImportTemplate"
	columns := []export.Column{
		{Key: "image_url", Label: "image_url"},
		{Key: "name", Label: "name*"},
		{Key: "item_type", Label: "item_type"},
		{Key: "sku", Label: "sku"},
		{Key: "barcode", Label: "barcode"},
		{Key: "category_id", Label: "category_id"},
		{Key: "unit", Label: "unit"},
		{Key: "selling_price", Label: "selling_price*"},
		{Key: "purchase_price", Label: "purchase_price"},
		{Key: "price_type", Label: "price_type"},
		{Key: "tax_id", Label: "tax_id"},
		{Key: "discount_type", Label: "discount_type"},
		{Key: "discount_value", Label: "discount_value"},
		{Key: "wholesale_prices", Label: "wholesale_prices"},
		{Key: "customer_type_prices", Label: "customer_type_prices"},
		{Key: "track_stock", Label: "track_stock"},
		{Key: "stock", Label: "stock"},
		{Key: "minimum_stock", Label: "minimum_stock"},
		{Key: "shelf_location", Label: "shelf_location"},
		{Key: "weight_grams", Label: "weight_grams"},
		{Key: "track_imei", Label: "track_imei"},
		{Key: "units", Label: "units"},
		{Key: "variants", Label: "variants"},
		{Key: "ingredients", Label: "ingredients"},
		{Key: "points_earned", Label: "points_earned"},
		{Key: "points_to_redeem", Label: "points_to_redeem"},
		{Key: "show_in_online_store", Label: "show_in_online_store"},
		{Key: "description", Label: "description"},
		{Key: "online_description", Label: "online_description"},
	}
	example := map[string]any{"item_type": "product", "selling_price": float64(0), "purchase_price": float64(0), "price_type": "single", "discount_type": "percent", "discount_value": float64(0), "track_stock": "true", "stock": float64(0), "minimum_stock": float64(0), "weight_grams": float64(0), "track_imei": "true", "points_earned": float64(0), "points_to_redeem": float64(0), "show_in_online_store": "true"}
	response, err := export.XLSX(helper.Lang(c), "Products", columns, []map[string]any{example})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "products-template.xlsx", xlsxContentType, response)
}

// V1Import godoc
//
//	@Summary		Import products
//	@Description	Upload an .xlsx or .csv file that follows the template. mode=create adds every row; mode=update changes the rows matched by sku. Each row is saved on its own, so one bad row does not stop the others.
//	@Tags			Products
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"create or update"	Enums(create, update)	default(create)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/import [post]
func (q *productController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Import"
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	update := sanitizers.ImportMode(c) == "update"
	response := models.ImportResult{Errors: []models.ImportError{}}
	for i, row := range rows {
		rowNo := i + 2 // the header is row 1
		created, err := q.importRow(c, row, update)
		switch {
		case err != nil:
			response.Failed++
			response.Errors = append(response.Errors, models.ImportError{Row: rowNo, Message: describeError(helper.Lang(c), productError(err))})
		case created:
			response.Created++
		default:
			response.Updated++
		}
	}
	return helper.OK(c, response)
}

// importRow saves one imported row in its own transaction; created tells an insert from an update.
func (q *productController) importRow(c fiber.Ctx, row map[string]any, update bool) (created bool, err error) {
	ctx := c.Context()
	ctxt := "ProductController-importRow"
	now := time.Now()
	product := &models.Product{
		BranchID:  currentBranchID(c),
		Actor:     currentActor(c),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if update {
		key := helper.Str(row, "sku")
		if key == "" {
			return false, helper.BadRequest("{key} is required to update", "key", "sku")
		}
		if product, err = q.productService.FindProduct(
			ctx,
			nil,
			models.NewProductFilter().
				WithBranchIDs(currentBranchID(c)).
				WithSKU(key),
		); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindProduct")
			if errors.Is(err, models.ErrProductNotFound) {
				err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Product"), "key", "sku", "value", key)
			}
			return false, err
		}
		product.UpdatedAt = now
	}
	request, err := sanitizers.ValidateProductRow(row, !update)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateProductRow")
		return false, err
	}
	request.Apply(product)
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return false, err
	}
	defer rollback(ctx, tx, ctxt)
	if update {
		_, err = q.productService.UpdateProduct(ctx, tx, product)
	} else {
		_, err = q.productService.CreateProduct(ctx, tx, product)
	}
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSaveProduct")
		return false, err
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return false, err
	}
	return !update, nil
}

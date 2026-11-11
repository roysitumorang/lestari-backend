package controllers

import (
	"log/slog"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
)

// maxPrintedProducts caps how many products one barcode sheet or label file holds.
const maxPrintedProducts int64 = 5000

// mountExtras registers the product routes beyond create, read, update and delete.
func (q *productController) mountExtras(private fiber.Router) {
	private.Get("/generate-sku", q.V1GenerateSKU)
	private.Get("/check-sku", q.V1CheckSKU)
	private.Get("/barcodes", q.V1Barcodes)
	private.Get("/labels", q.V1Labels)
	private.Post("/:id<guid>/duplicate", q.V1Duplicate)
}

// V1GenerateSKU godoc
//
//	@Summary		Suggest the next SKU
//	@Description	The SKU a new product gets when none is sent: P followed by the next free six-digit number.
//	@Tags			Products
//	@Success		200	{object}	helper.Response{data=models.SKUResponse}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/generate-sku [get]
func (q *productController) V1GenerateSKU(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1GenerateSKU"
	sku, err := q.productService.NextSKU(ctx, currentBranchID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrNextSKU")
		return err
	}
	return helper.OK(c, models.SKUResponse{SKU: sku})
}

// V1CheckSKU godoc
//
//	@Summary	Check whether a SKU is free
//	@Tags		Products
//	@Param		sku			query		string	true	"SKU to check (case-insensitive)"
//	@Param		exclude_id	query		string	false	"product id (UUID) to leave out, when editing that product"
//	@Success	200			{object}	helper.Response{data=models.AvailabilityResponse}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Failure	422			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/products/check-sku [get]
func (q *productController) V1CheckSKU(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1CheckSKU"
	sku, exceptProductID, err := sanitizers.CheckSKU(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCheckSKU")
		return err
	}
	available, err := q.productService.SKUAvailable(ctx, currentBranchID(c), sku, exceptProductID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSKUAvailable")
		return err
	}
	return helper.OK(c, models.AvailabilityResponse{Available: available})
}

// V1Duplicate godoc
//
//	@Summary		Duplicate a product
//	@Description	Creates a copy named "<name> (copy)" with a new automatic SKU, no barcode and no stock.
//	@Tags			Products
//	@Param			id	path		string	true	"product id (UUID)"
//	@Success		201	{object}	helper.Response{data=models.Product}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/{id}/duplicate [post]
func (q *productController) V1Duplicate(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Duplicate"
	productID, err := sanitizers.ParamID(c, "id", "Product")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
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
	now := time.Now()
	product.ID = uuid.Nil()
	product.Actor = currentActor(c)
	product.Name = helper.T(c, "{name} (copy)", "name", product.Name)
	product.SKU = ""
	product.Barcode = nil
	product.Stock = 0
	product.CreatedAt = now
	product.UpdatedAt = now
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

// printedProducts returns the products named by ?product_ids[]= (all of the branch when none), by name.
func (q *productController) printedProducts(c fiber.Ctx) ([]*models.Product, error) {
	response, _, err := q.productService.FindProducts(
		c.Context(),
		models.NewProductFilter().
			WithBranchIDs(currentBranchID(c)).
			WithProductIDs(sanitizers.ProductIDs(c)...).
			WithSortings(models.Sorting{Field: "name", Direction: models.SortingDirectionAsc}).
			WithLimit(maxPrintedProducts).
			WithPage(1),
	)
	return response, err
}

// printedCode is what a barcode encodes: the barcode of the product, or its SKU when it has none.
func printedCode(product *models.Product) string {
	if product.Barcode != nil && *product.Barcode != "" {
		return *product.Barcode
	}
	return product.SKU
}

// V1Barcodes godoc
//
//	@Summary		Print barcodes
//	@Description	A printable PDF sheet with one barcode (or QR code) per product; a product without a barcode uses its SKU.
//	@Tags			Products
//	@Produce		application/pdf
//	@Param			product_ids[]	query		[]string	false	"product ids (UUID); all products when empty"	collectionFormat(multi)
//	@Param			copies			query		int			false	"copies of each product"						default(1)			minimum(1)	maximum(100)
//	@Param			symbology		query		string		false	"code type"										Enums(code128, qr)	default(code128)
//	@Param			show_price		query		bool		false	"print the selling price"						default(true)
//	@Param			paper_size		query		string		false	"sheet or label size"							Enums(a4, label_33x15, label_50x25)	default(a4)
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/barcodes [get]
func (q *productController) V1Barcodes(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Barcodes"
	products, err := q.printedProducts(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPrintedProducts")
		return err
	}
	sheet := sanitizers.BarcodeSheet(c)
	var width, height, cellWidth, cellHeight float64 = 595, 842, 180, 90 // A4 grid, in points
	switch sheet.PaperSize {
	case models.PaperSizeLabel33x15:
		width, height, cellWidth, cellHeight = 93.5, 42.5, 93.5, 42.5
	case models.PaperSizeLabel50x25:
		width, height, cellWidth, cellHeight = 141.7, 70.9, 141.7, 70.9
	}
	document := export.NewPDF(width, height)
	margin := 18.0
	if width < 200 {
		margin = 0
	}
	columns := max(int((width-2*margin)/cellWidth), 1)
	perPage := max(columns*int((height-2*margin)/cellHeight), 1)
	printed := 0
	for _, product := range products {
		code := printedCode(product)
		for range sheet.Copies {
			if printed%perPage == 0 {
				document.Page()
			}
			slot := printed % perPage
			x := margin + float64(slot%columns)*cellWidth
			top := height - margin - float64(slot/columns)*cellHeight
			fontSize := cellHeight / 9
			document.Text(x+4, top-fontSize-2, fontSize, true, export.Fit(product.Name, cellWidth-8, fontSize))
			if sheet.Symbology == models.BarcodeSymbologyQR {
				if qr, err := qrcode.New(code, qrcode.Medium); err == nil {
					size := cellHeight * 0.55
					document.Matrix(x+4, top-fontSize-6-size, size, qr.Bitmap())
				}
			} else {
				document.Barcode(x+6, top-cellHeight*0.62, cellWidth-12, cellHeight*0.38, code)
			}
			document.Text(x+4, top-cellHeight+fontSize*0.4+2, fontSize*0.85, false, code)
			if sheet.ShowPrice {
				document.TextRight(x+cellWidth-4, top-cellHeight+fontSize*0.4+2, fontSize*0.85, true, "Rp "+export.Thousands(product.SellingPrice))
			}
			printed++
		}
	}
	if printed == 0 {
		document.Text(40, height-60, 12, false, helper.T(c, "No products selected."))
	}
	return export.Send(c, "barcodes.pdf", "application/pdf", document.Bytes())
}

// V1Labels godoc
//
//	@Summary		Download price labels
//	@Description	An .xlsx file with the name, SKU, barcode, price, unit and category of the chosen products, for label software.
//	@Tags			Products
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
//	@Param			product_ids[]	query		[]string	false	"product ids (UUID); all products when empty"	collectionFormat(multi)
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/products/labels [get]
func (q *productController) V1Labels(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ProductController-V1Labels"
	products, err := q.printedProducts(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPrintedProducts")
		return err
	}
	rows := make([]map[string]any, len(products))
	for i, product := range products {
		row := map[string]any{
			"name":          product.Name,
			"sku":           product.SKU,
			"barcode":       printedCode(product),
			"selling_price": product.SellingPrice,
		}
		if product.Unit != nil {
			row["unit"] = *product.Unit
		}
		if product.Category != nil {
			row["category_name"] = product.Category.Name
		}
		rows[i] = row
	}
	columns := []export.Column{
		{Key: "name", Label: "Name"},
		{Key: "sku", Label: "SKU"},
		{Key: "barcode", Label: "Barcode"},
		{Key: "selling_price", Label: "Price", Format: "money"},
		{Key: "unit", Label: "Unit"},
		{Key: "category_name", Label: "Category"},
	}
	response, err := export.XLSX(helper.Lang(c), "Labels", columns, rows)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrXLSX")
		return err
	}
	return export.Send(c, "labels.xlsx", xlsxContentType, response)
}

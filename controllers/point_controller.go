package controllers

import (
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type pointController struct {
	authService  *middlewares.Service
	pointService services.PointService
}

func NewPointController(
	authService *middlewares.Service,
	pointService services.PointService,
) *pointController {
	return &pointController{
		authService:  authService,
		pointService: pointService,
	}
}

// Mount registers the point and QR-card routes under /customers.
func (q *pointController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("/qr-codes", q.V1QRCodes)
	private.Post("/points/import", q.V1Import)
	private.Post("/:id<guid>/points", q.V1Adjust)
	private.Get("/:id<guid>/point-history", q.V1History)
}

// V1Adjust godoc
//
//	@Summary		Adjust the points of a customer
//	@Description	Adds or subtracts points and records the change in the point history. The balance never goes below zero.
//	@Tags			Customers
//	@Param			id		path		string							true	"customer id (UUID)"
//	@Param			request	body		models.PointAdjustmentRequest	true	"adjustment"
//	@Success		200		{object}	helper.Response{data=models.PointsResponse}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/{id}/points [post]
func (q *pointController) V1Adjust(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PointController-V1Adjust"
	customerID, err := sanitizers.ParamID(c, "id", "Customer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	points, note, err := sanitizers.PointAdjustment(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrPointAdjustment")
		return err
	}
	var response models.PointsResponse
	if err = inTx(c, ctxt, func(tx pgx.Tx) (err error) {
		if response.Points, err = q.pointService.ChangePoints(ctx, tx, &models.PointChange{
			BranchID:   currentBranchID(c),
			CustomerID: customerID,
			Points:     points,
			Type:       models.PointTypeAdjusted,
			Note:       note,
			At:         time.Now(),
		}); err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrChangePoints")
		}
		return customerError(err)
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1History godoc
//
//	@Summary	List the point history of a customer
//	@Tags		Customers
//	@Param		id			path		string	true	"customer id (UUID)"
//	@Param		page		query		int		false	"page, from 1"	default(1)
//	@Param		per_page	query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success	200			{object}	helper.Response{data=helper.Pagination{rows=[]models.PointEntry}}
//	@Failure	401			{object}	helper.Response{data=helper.ErrorData}
//	@Router		/customers/{id}/point-history [get]
func (q *pointController) V1History(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PointController-V1History"
	customerID, err := sanitizers.ParamID(c, "id", "Customer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	limit, pageNo := sanitizers.PageOf(c, 50)
	response, total, err := q.pointService.FindPointEntries(ctx, currentBranchID(c), customerID, limit, pageNo)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindPointEntries")
		return err
	}
	return helper.List(c, response, page(limit, pageNo, total))
}

// V1QRCodes godoc
//
//	@Summary		Print customer QR cards
//	@Description	A printable PDF with the QR code of each customer (their code, or one derived from their id), as a grid or as cards.
//	@Tags			Customers
//	@Produce		application/pdf
//	@Param			customer_ids[]	query		[]string	false	"customer ids (UUID); all customers when empty"	collectionFormat(multi)
//	@Param			layout			query		string		false	"grid (default) or card"						Enums(grid, card)
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/qr-codes [get]
func (q *pointController) V1QRCodes(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PointController-V1QRCodes"
	customerIDs := []uuid.UUID{}
	for _, item := range helper.QueryArray(c, "customer_ids") {
		if customerID, err := uuid.Parse(strings.TrimSpace(item)); err == nil {
			customerIDs = append(customerIDs, customerID)
		}
	}
	customers, err := q.pointService.FindCustomerCodes(ctx, currentBranchID(c), customerIDs)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindCustomerCodes")
		return err
	}
	document := export.NewPDF(595, 842)
	card := c.Query("layout") == "card"
	cellWidth, cellHeight := 180.0, 200.0
	if card {
		cellWidth, cellHeight = 260, 150
	}
	columns := int((595 - 36) / cellWidth)
	perPage := columns * int((842-36)/cellHeight)
	for i, customer := range customers {
		if i%perPage == 0 {
			document.Page()
		}
		slot := i % perPage
		x := 18 + float64(slot%columns)*cellWidth
		top := 824 - float64(slot/columns)*cellHeight
		size := cellHeight - 60
		if card {
			size = cellHeight - 30
		}
		if code, err := qrcode.New(customer.Code, qrcode.Medium); err == nil {
			document.Matrix(x+6, top-size-6, size, code.Bitmap())
		}
		// a card has the text beside the code, the grid below it
		textX, textY, textWidth := x+6, top-size-22, cellWidth-20
		if card {
			textX, textY, textWidth = x+size+16, top-30, cellWidth-size-20
		}
		document.Text(textX, textY, 10, true, export.Fit(customer.Name, textWidth, 10))
		document.Text(textX, textY-14, 9, false, customer.Code)
	}
	if len(customers) == 0 {
		document.Text(40, 780, 12, false, helper.T(c, "No customers selected."))
	}
	return export.Send(c, "customer-qr-codes.pdf", "application/pdf", document.Bytes())
}

// V1Import godoc
//
//	@Summary		Import customer points
//	@Description	Upload an .xlsx or .csv file with the columns code and points. mode=add (default) adds the points to each customer, mode=set makes them the balance. A row with an unknown code is reported and skipped.
//	@Tags			Customers
//	@Accept			multipart/form-data
//	@Param			file	formData	file	true	".xlsx or .csv file"
//	@Param			mode	formData	string	false	"add or set"	Enums(add, set)	default(add)
//	@Success		200		{object}	helper.Response{data=models.ImportResult}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/customers/points/import [post]
func (q *pointController) V1Import(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "PointController-V1Import"
	rows, err := sanitizers.ImportRows(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportRows")
		return err
	}
	set := c.FormValue("mode", "add") == "set"
	now := time.Now()
	response := models.ImportResult{Errors: []models.ImportError{}}
	if err = inTx(c, ctxt, func(tx pgx.Tx) error {
		for i, row := range rows {
			code := helper.Str(row, "code")
			points, ok := helper.Num(row, "points")
			var err error
			if code == "" || !ok {
				err = helper.BadRequest("code and points are required")
			} else if err = q.pointService.ImportPoints(ctx, tx, &models.PointImport{
				BranchID: currentBranchID(c),
				Code:     code,
				Points:   int64(points),
				Set:      set,
				At:       now,
			}); errors.Is(err, models.ErrCustomerNotFound) {
				err = helper.BadRequest("No {item} with {key} {value}", "item", i18n.Tr("Customer"), "key", "code", "value", code)
			}
			if err == nil {
				response.Updated++
				continue
			}
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrImportPoints")
			if he := (&helper.Error{}); !errors.As(err, &he) {
				// not a problem of this row: nothing of the file is saved
				return err
			}
			response.Failed++
			response.Errors = append(response.Errors, models.ImportError{Row: i + 2, Message: describeError(helper.Lang(c), err)})
		}
		return nil
	}); err != nil {
		return err
	}
	return helper.OK(c, response)
}

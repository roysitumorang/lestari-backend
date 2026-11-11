package controllers

import (
	"errors"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/gofiber/fiber/v3"
	"github.com/jackc/pgx/v5"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/repositories"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type stockTransferController struct {
	authService          *middlewares.Service
	stockTransferService services.StockTransferService
}

func NewStockTransferController(
	authService *middlewares.Service,
	stockTransferService services.StockTransferService,
) *stockTransferController {
	return &stockTransferController{
		authService:          authService,
		stockTransferService: stockTransferService,
	}
}

// Mount registers the routes of /stock-transfers.
func (q *stockTransferController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	private.Get("", q.V1Index)
	private.Post("", q.V1Create)
	private.Get("/:id<guid>", q.V1Show)
	private.Delete("/:id<guid>", q.V1Delete)
	private.Post("/:id<guid>/accept", q.V1Accept)
	private.Post("/:id<guid>/reject", q.V1Reject)
	private.Get("/:id<guid>/print", q.V1Print)
}

// stockTransferError turns the stock transfer errors into responses; anything else goes to the error handler as it is.
func stockTransferError(err error) error {
	switch {
	case errors.Is(err, models.ErrStockTransferNotFound):
		return helper.NotFound("Stock transfer")
	case errors.Is(err, models.ErrProductNotFound):
		return helper.NotFound("Product")
	}
	return writeError(err)
}

// currentHeadOfficeID is the head-office branch of the signed-in user's store.
func currentHeadOfficeID(c fiber.Ctx) uuid.UUID {
	id, _ := uuid.Parse(helper.P(c).HeadOffice)
	return id
}

// visibleStockTransfer selects one transfer the active branch sends or receives.
func visibleStockTransfer(c fiber.Ctx, stockTransferID uuid.UUID) *models.StockTransferFilter {
	return models.NewStockTransferFilter().
		WithStoreIDs(currentStoreID(c)).
		WithPartyBranchIDs(currentBranchID(c)).
		WithStockTransferIDs(stockTransferID)
}

// V1Index godoc
//
//	@Summary		List stock transfers
//	@Description	The transfers the active branch sends or receives.
//	@Tags			Stock transfers
//	@Param			q			query		string	false	"search in transfer_number"
//	@Param			status		query		string	false	"only rows with this status"									Enums(pending, completed, accepted, rejected)
//	@Param			direction	query		string	false	"out: sent by the active branch; in: sent to it"				Enums(in, out)
//	@Param			role		query		string	false	"head_office: the head office takes part; branch: it does not"	Enums(head_office, branch)
//	@Param			start_date	query		string	false	"transfers on or after this date (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"transfers on or before this date (YYYY-MM-DD)"
//	@Param			sort		query		string	false	"sort field"		Enums(transfer_number, transfer_date, status, total_quantity, item_count, from_branch.name, to_branch.name, created_at, updated_at)
//	@Param			order		query		string	false	"sort direction"	Enums(asc, desc)	default(asc)
//	@Param			page		query		int		false	"page, from 1"		default(1)
//	@Param			per_page	query		int		false	"rows per page"		default(25)	maximum(1000)
//	@Success		200			{object}	helper.Response{data=helper.Pagination{rows=[]models.StockTransfer}}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-transfers [get]
func (q *stockTransferController) V1Index(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Index"
	branchID := currentBranchID(c)
	request := sanitizers.FindStockTransfers(c).
		WithStoreIDs(currentStoreID(c)).
		WithPartyBranchIDs(branchID)
	switch sanitizers.StockTransferDirection(c) {
	case models.StockTransferDirectionOut:
		request.WithFromBranchIDs(branchID)
	case models.StockTransferDirectionIn:
		request.WithToBranchIDs(branchID)
	}
	if role := sanitizers.StockTransferRole(c); role != "" {
		request.WithRole(role, currentHeadOfficeID(c))
	}
	response, total, err := q.stockTransferService.FindStockTransfers(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfers")
		return err
	}
	for _, stockTransfer := range response {
		stockTransfer.SeenFrom(branchID)
	}
	return helper.List(c, response, helper.NewMeta(helper.Page{Page: int(request.Page), PerPage: int(request.Limit)}, int(total)))
}

// V1Show godoc
//
//	@Summary	Get one stock transfer
//	@Tags		Stock transfers
//	@Param		id	path		string	true	"stock transfer id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.StockTransfer}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/stock-transfers/{id} [get]
func (q *stockTransferController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Show"
	stockTransferID, err := sanitizers.ParamID(c, "id", "Stock transfer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.stockTransferService.FindStockTransfer(ctx, nil, visibleStockTransfer(c, stockTransferID))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfer")
		return stockTransferError(err)
	}
	return helper.OK(c, response.SeenFrom(currentBranchID(c)))
}

// V1Create godoc
//
//	@Summary		Create a stock transfer
//	@Description	Sends stock from the active branch to another branch of the store. The stock leaves the sender at once. Without confirmation it also arrives at once (status completed) and a product the receiver does not have yet is created there; with confirmation the transfer stays pending until the receiver accepts or rejects it.
//	@Tags			Stock transfers
//	@Param			request	body		models.StockTransferRequest	true	"stock transfer"
//	@Success		201		{object}	helper.Response{data=models.StockTransfer}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-transfers [post]
func (q *stockTransferController) V1Create(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Create"
	stockTransfer, err := sanitizers.ValidateStockTransfer(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStockTransfer")
		return err
	}
	now := time.Now()
	stockTransfer.StoreID = currentStoreID(c)
	stockTransfer.FromBranchID = currentBranchID(c)
	stockTransfer.Actor = currentActor(c)
	stockTransfer.CreatedAt = now
	stockTransfer.UpdatedAt = now
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	response, err := q.stockTransferService.CreateStockTransfer(ctx, tx, stockTransfer)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCreateStockTransfer")
		return stockTransferError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.Created(c, response.SeenFrom(currentBranchID(c)))
}

// decision reads the transfer of the path as a decision of the signed-in user in the active branch.
func (q *stockTransferController) decision(c fiber.Ctx, ctxt string) (*models.StockTransferDecision, error) {
	stockTransferID, err := sanitizers.ParamID(c, "id", "Stock transfer")
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return nil, err
	}
	return &models.StockTransferDecision{
		StockTransferID: stockTransferID,
		StoreID:         currentStoreID(c),
		BranchID:        currentBranchID(c),
		Actor:           currentActor(c),
		At:              time.Now(),
	}, nil
}

// decide runs an accept or reject in a transaction and returns the transfer as it is afterwards.
func (q *stockTransferController) decide(c fiber.Ctx, ctxt string, request *models.StockTransferDecision, apply func(tx pgx.Tx) error) error {
	ctx := c.Context()
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = apply(tx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDecide")
		return stockTransferError(err)
	}
	response, err := q.stockTransferService.FindStockTransfer(ctx, tx, visibleStockTransfer(c, request.StockTransferID))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfer")
		return stockTransferError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.OK(c, response.SeenFrom(request.BranchID))
}

// V1Accept godoc
//
//	@Summary		Accept a stock transfer
//	@Description	Only the receiving branch can accept, and only while the transfer is pending. The received stock is added to the branch; a quantity that did not arrive goes back to the sender.
//	@Tags			Stock transfers
//	@Param			id		path		string								true	"stock transfer id (UUID)"
//	@Param			request	body		models.StockTransferAcceptRequest	false	"quantities received, when they differ from what was sent"
//	@Success		200		{object}	helper.Response{data=models.StockTransfer}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-transfers/{id}/accept [post]
func (q *stockTransferController) V1Accept(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Accept"
	request, err := q.decision(c, ctxt)
	if err != nil {
		return err
	}
	if request.Received, request.Note, err = sanitizers.ValidateStockTransferAccept(c); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStockTransferAccept")
		return err
	}
	return q.decide(c, ctxt, request, func(tx pgx.Tx) error {
		return q.stockTransferService.AcceptStockTransfer(ctx, tx, request)
	})
}

// V1Reject godoc
//
//	@Summary		Reject a stock transfer
//	@Description	Only the receiving branch can reject, and only while the transfer is pending. The stock goes back to the sender.
//	@Tags			Stock transfers
//	@Param			id		path		string								true	"stock transfer id (UUID)"
//	@Param			request	body		models.StockTransferRejectRequest	true	"why it is rejected"
//	@Success		200		{object}	helper.Response{data=models.StockTransfer}
//	@Failure		401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409		{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422		{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-transfers/{id}/reject [post]
func (q *stockTransferController) V1Reject(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Reject"
	request, err := q.decision(c, ctxt)
	if err != nil {
		return err
	}
	if request.Reason, err = sanitizers.ValidateStockTransferReject(c); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrValidateStockTransferReject")
		return err
	}
	return q.decide(c, ctxt, request, func(tx pgx.Tx) error {
		return q.stockTransferService.RejectStockTransfer(ctx, tx, request)
	})
}

// V1Delete godoc
//
//	@Summary		Delete a stock transfer
//	@Description	Only the sending branch can delete, and only while the transfer is pending. The stock goes back to the sender.
//	@Tags			Stock transfers
//	@Param			id	path	string	true	"stock transfer id (UUID)"
//	@Success		204
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		403	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		409	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/stock-transfers/{id} [delete]
func (q *stockTransferController) V1Delete(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Delete"
	request, err := q.decision(c, ctxt)
	if err != nil {
		return err
	}
	tx, err := repositories.BeginTx(ctx)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrBeginTx")
		return err
	}
	defer rollback(ctx, tx, ctxt)
	if err = q.stockTransferService.DeleteStockTransfer(ctx, tx, request); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrDeleteStockTransfer")
		return stockTransferError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrCommit")
		return err
	}
	return helper.NoContent(c)
}

// V1Print godoc
//
//	@Summary	Print a stock transfer note
//	@Tags		Stock transfers
//	@Produce	application/pdf
//	@Param		id	path		string	true	"stock transfer id (UUID)"
//	@Success	200	{file}		binary
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/stock-transfers/{id}/print [get]
func (q *stockTransferController) V1Print(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "StockTransferController-V1Print"
	stockTransferID, err := sanitizers.ParamID(c, "id", "Stock transfer")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	stockTransfer, err := q.stockTransferService.FindStockTransfer(ctx, nil, visibleStockTransfer(c, stockTransferID))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStockTransfer")
		return stockTransferError(err)
	}
	stockTransfer.SeenFrom(currentBranchID(c))
	t := helper.Lang(c).T
	document := export.NewPDF(595, 842)
	document.Page()
	document.Text(40, 790, 16, true, t("STOCK TRANSFER"))
	document.Text(40, 770, 10, false, t("No")+": "+stockTransfer.TransferNumber)
	document.Text(40, 756, 10, false, t("Date")+": "+stockTransfer.TransferDate)
	document.Text(40, 742, 10, false, t("From")+": "+stockTransfer.FromBranchName+"   "+t("To")+": "+stockTransfer.ToBranchName)
	document.Text(40, 728, 10, false, t("Status")+": "+t(stockTransfer.Status))
	y := 700.0
	document.Rect(40, y-4, 515, 16, 0.92)
	document.Text(46, y, 9, true, t("Product"))
	document.Text(300, y, 9, true, "SKU")
	document.Text(420, y, 9, true, t("Qty"))
	document.Text(480, y, 9, true, t("Received"))
	y -= 20
	for _, item := range stockTransfer.Items {
		if y < 100 {
			document.Page()
			y = 800
		}
		document.Text(46, y, 9, false, export.Fit(item.ProductName, 240, 9))
		if item.SKU != nil {
			document.Text(300, y, 9, false, *item.SKU)
		}
		document.Text(420, y, 9, false, export.Thousands(item.Quantity))
		if item.ReceivedQuantity != nil {
			document.Text(480, y, 9, false, export.Thousands(*item.ReceivedQuantity))
		}
		y -= 15
	}
	if stockTransfer.Note != nil && *stockTransfer.Note != "" {
		document.Text(40, y-10, 9, false, t("Note")+": "+export.Fit(*stockTransfer.Note, 500, 9))
	}
	y -= 70
	document.Line(40, y, 200)
	document.Text(40, y-12, 9, false, t("Sent by"))
	document.Line(355, y, 515)
	document.Text(355, y-12, 9, false, t("Received by"))
	return export.Send(c, "transfer-"+strings.ReplaceAll(stockTransfer.TransferNumber, "/", "-")+".pdf", "application/pdf", document.Bytes())
}

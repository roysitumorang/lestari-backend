package controllers

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"time"
	"uuid"

	"github.com/goccy/go-json"
	"github.com/gofiber/fiber/v3"

	"github.com/roysitumorang/lestari-backend/export"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/i18n"
	"github.com/roysitumorang/lestari-backend/middlewares"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/sanitizers"
	"github.com/roysitumorang/lestari-backend/services"
)

type reportController struct {
	authService   *middlewares.Service
	reportService services.ReportService
	shiftService  services.ShiftService
	jobService    services.JobService
}

func NewReportController(
	authService *middlewares.Service,
	reportService services.ReportService,
	shiftService services.ShiftService,
	jobService services.JobService,
) *reportController {
	return &reportController{
		authService:   authService,
		reportService: reportService,
		shiftService:  shiftService,
		jobService:    jobService,
	}
}

// Mount registers the report routes under /reports.
func (q *reportController) Mount(r fiber.Router) {
	private := r.Group("", q.authService.Middleware())
	q.mountReports(private)
	private.Get("/shifts", q.V1Shifts)
	private.Get("/shifts/export", q.V1ShiftsExport)
	private.Get("/shifts/:id<guid>", q.V1Shift)
	private.Get("/shifts/:id<guid>/export", q.V1ShiftPDF)
	private.Post("/daily-sales/rebuild", q.V1RebuildDailySales)
	private.Get("/daily-sales/:date", q.V1SalesOfDay)
	private.Get("/sales-by-product/:product_id<guid>", q.V1SalesOfProduct)
	private.Get("/customer-points/:customer_id<guid>", q.V1SalesOfCustomerPoints)
	private.Get("/point-redemptions/:product_id<guid>", q.V1SalesOfRedemption)
	private.Get("/point-earnings/:product_id<guid>", q.V1SalesOfEarning)
	private.Get("/promotions/transactions/:promotion_id<guid>", q.V1SalesOfPromotion)
	private.Get("/promotions/products/:product_id<guid>", q.V1SalesOfPromotionProduct)
	private.Get("/store-ratings/:id<guid>", q.V1StoreRating)
}

// reportFilter reads the parameters of a report of the signed-in user's store and active branch.
func reportFilter(c fiber.Ctx, ctxt string, dated bool) (*models.ReportFilter, error) {
	response, err := sanitizers.FindReport(c, dated)
	if err != nil {
		helper.Log(c.Context(), slog.LevelError, err.Error(), ctxt, "ErrFindReport")
		return nil, err
	}
	response.StoreID, response.BranchID = currentStoreID(c), currentBranchID(c)
	return response, nil
}

// reportList answers a report: its rows (a page of them when the report is paged), its summary and, for the
// reports that have one, the totals row.
func reportList[R, S any](
	c fiber.Ctx,
	ctxt string,
	dated,
	paged,
	totals bool,
	find func(ctx context.Context, f *models.ReportFilter) ([]*R, *S, int64, error),
) error {
	ctx := c.Context()
	request, err := reportFilter(c, ctxt, dated)
	if err != nil {
		return err
	}
	response, summary, total, err := find(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFind")
		return err
	}
	meta := helper.Unpaged(len(response))
	if paged {
		meta = page(request.Limit, request.Page, total)
	}
	var extras helper.Extras
	if summary != nil {
		extras.Summary = summary
		if totals {
			extras.Totals = summary
		}
	}
	return helper.ListWith(c, response, meta, extras)
}

// reportExport writes every row of a report as a file; the totals row, when the report has one, closes it.
func reportExport[R, S any](
	c fiber.Ctx,
	ctxt string,
	dated,
	totals bool,
	name,
	title string,
	columns []export.Column,
	find func(ctx context.Context, f *models.ReportFilter) ([]*R, *S, int64, error),
) error {
	ctx := c.Context()
	request, err := reportFilter(c, ctxt, dated)
	if err != nil {
		return err
	}
	request.All = true
	response, summary, _, err := find(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFind")
		return err
	}
	rows, err := exportRows(response)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
		return err
	}
	if totals && summary != nil {
		sums, err := exportRows([]*S{summary})
		if err != nil {
			helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrExportRows")
			return err
		}
		last := maps.Clone(sums[0])
		last[columns[0].Key] = "Total"
		rows = append(rows, last)
	}
	if err = export.Write(c, name, title, columns, rows); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrWrite")
	}
	return err
}

var shiftColumns = []export.Column{
	{Key: "opened_at", Label: "Opened", Format: "datetime"},
	{Key: "closed_at", Label: "Closed", Format: "datetime"},
	{Key: "cash_drawer_name", Label: "Drawer"},
	{Key: "cashier_name", Label: "Cashier"},
	{Key: "opening_cash", Label: "Opening", Format: "money"},
	{Key: "expected_cash", Label: "Expected", Format: "money"},
	{Key: "actual_cash", Label: "Counted", Format: "money"},
	{Key: "difference", Label: "Difference", Format: "money"},
}

// findShifts adapts the shift list to the shape of a report.
func (q *reportController) findShifts(ctx context.Context, f *models.ReportFilter) ([]*models.Shift, *models.ReportNoSummary, int64, error) {
	request := &models.ShiftFilter{
		BranchID:     f.BranchID,
		CashDrawerID: f.CashDrawerID,
		CashierID:    f.CashierID,
		StartDate:    f.StartDate,
		EndDate:      f.EndDate,
		Limit:        f.Limit,
		Page:         f.Page,
	}
	if f.All {
		request.Limit, request.Page = 100000, 1
	}
	response, total, err := q.shiftService.FindShifts(ctx, request)
	return response, nil, total, err
}

// V1Shifts godoc
//
//	@Summary		Cashier shifts
//	@Description	The shifts opened in the period with their expected and counted cash.
//	@Tags			Reports
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cash_drawer_id	query		string	false	"only this cash drawer (UUID)"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Param			page			query		int		false	"page, from 1"	default(1)
//	@Param			per_page		query		int		false	"rows per page"	default(50)	maximum(1000)
//	@Success		200				{object}	helper.Response{data=helper.Pagination{rows=[]models.Shift}}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/shifts [get]
func (q *reportController) V1Shifts(c fiber.Ctx) error {
	return reportList(c, "ReportController-V1Shifts", true, true, false, q.findShifts)
}

// V1ShiftsExport godoc
//
//	@Summary		Export: cashier shifts
//	@Description	Every row of the report (same filters) as a file.
//	@Tags			Reports
//	@Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet,text/csv,application/pdf
//	@Param			format			query		string	false	"file format"	Enums(xlsx, csv, pdf)	default(xlsx)
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD); today by default"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Param			cash_drawer_id	query		string	false	"only this cash drawer (UUID)"
//	@Param			cashier_id		query		string	false	"only this cashier (UUID)"
//	@Success		200				{file}		binary
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/shifts/export [get]
func (q *reportController) V1ShiftsExport(c fiber.Ctx) error {
	return reportExport(c, "ReportController-V1ShiftsExport", true, false, "shifts", "Shifts", shiftColumns, q.findShifts)
}

// findShift reads the shift of the path.
func findShift(c fiber.Ctx, ctxt string, shiftService services.ShiftService) (*models.Shift, error) {
	ctx := c.Context()
	shiftID, err := sanitizers.ParamID(c, "id", "Shift")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return nil, err
	}
	response, err := shiftService.FindShift(ctx, currentBranchID(c), shiftID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindShift")
		return nil, shiftError(err)
	}
	return response, nil
}

// shiftError turns the shift errors into responses.
func shiftError(err error) error {
	if errors.Is(err, models.ErrShiftNotFound) {
		return helper.NotFound("Shift")
	}
	return writeError(err)
}

// V1Shift godoc
//
//	@Summary		Show a cashier shift
//	@Description	A shift with its cash movements and its sales per payment method.
//	@Tags			Reports
//	@Param			id	path		string	true	"shift id (UUID)"
//	@Success		200	{object}	helper.Response{data=models.Shift}
//	@Failure		401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure		404	{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/shifts/{id} [get]
func (q *reportController) V1Shift(c fiber.Ctx) error {
	response, err := findShift(c, "ReportController-V1Shift", q.shiftService)
	if err != nil {
		return err
	}
	return helper.OK(c, response)
}

// V1ShiftPDF godoc
//
//	@Summary	Print a cashier shift
//	@Tags		Reports
//	@Produce	application/pdf
//	@Param		id	path		string	true	"shift id (UUID)"
//	@Success	200	{file}		binary
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/reports/shifts/{id}/export [get]
func (q *reportController) V1ShiftPDF(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ReportController-V1ShiftPDF"
	shift, err := findShift(c, ctxt, q.shiftService)
	if err != nil {
		return err
	}
	tr := helper.Lang(c).T
	document := export.NewPDF(226, 420)
	document.Page()
	y := 396.0
	line := func(label, value string, bold bool) {
		document.Text(10, y, 8, bold, label)
		document.TextRight(216, y, 8, bold, value)
		y -= 13
	}
	money := func(value *float64) string {
		if value == nil {
			return "-"
		}
		return export.Thousands(*value)
	}
	text := func(value *string) string {
		if value == nil {
			return "-"
		}
		return *value
	}
	closed := "-"
	if shift.ClosedAt != nil {
		closed = formatTime(*shift.ClosedAt)
	}
	document.Text(10, y, 11, true, tr("SHIFT REPORT"))
	y -= 18
	line(tr("Cash drawer"), text(shift.CashDrawerName), false)
	line(tr("Cashier"), text(shift.CashierName), false)
	line(tr("Opened"), formatTime(shift.OpenedAt), false)
	line(tr("Closed"), closed, false)
	document.Line(10, y+6, 216)
	y -= 4
	line(tr("Opening cash"), money(&shift.OpeningCash), false)
	line(tr("Cash sales"), money(&shift.CashSales), false)
	line(tr("Cash in"), money(&shift.CashIn), false)
	line(tr("Cash out"), money(&shift.CashOut), false)
	line(tr("Expected cash"), money(&shift.ExpectedCash), true)
	line(tr("Counted cash"), money(shift.ActualCash), true)
	line(tr("Difference"), money(shift.Difference), true)
	document.Line(10, y+6, 216)
	y -= 4
	for _, payment := range shift.Payments {
		line(payment.PaymentMethod+" ("+export.Thousands(float64(payment.TransactionCount))+")", money(&payment.Amount), false)
	}
	if err = export.Send(c, "shift.pdf", "application/pdf", document.Bytes()); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrSend")
	}
	return err
}

// V1RebuildDailySales godoc
//
//	@Summary		Rebuild the daily sales
//	@Description	Starts a background job. The daily totals are worked out from the transactions on every read, so the job only confirms the period; poll GET /jobs/{id}.
//	@Tags			Reports
//	@Param			start_date	query		string	false	"first day to rebuild (YYYY-MM-DD)"
//	@Success		200			{object}	helper.Response{data=models.JobResponse}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/daily-sales/rebuild [post]
func (q *reportController) V1RebuildDailySales(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ReportController-V1RebuildDailySales"
	payload, err := json.Marshal(map[string]any{"start_date": c.Query("start_date"), "branch_id": currentBranchID(c)})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrMarshal")
		return err
	}
	response, err := q.jobService.StartJob(ctx, &models.Job{
		StoreID:   currentStoreID(c),
		Kind:      "rebuild_daily_sales",
		Payload:   payload,
		CreatedAt: time.Now(),
	}, func(context.Context) (any, error) {
		return map[string]any{"rebuilt": true}, nil
	})
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrStartJob")
		return err
	}
	return helper.OK(c, response)
}

// V1SalesOfDay godoc
//
//	@Summary	List the sales of a day
//	@Tags		Reports
//	@Param		date	path		string	true	"day (YYYY-MM-DD)"
//	@Success	200		{object}	helper.Response{data=models.ReportSales}
//	@Failure	401		{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404		{object}	helper.Response{data=helper.ErrorData}
//	@Router		/reports/daily-sales/{date} [get]
func (q *reportController) V1SalesOfDay(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ReportController-V1SalesOfDay"
	day := c.Params("date")
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		err = helper.NotFound("Day")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParse")
		return err
	}
	response, err := q.reportService.FindSalesOfDay(ctx, currentBranchID(c), day)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesOfDay")
		return err
	}
	return helper.OK(c, models.ReportSales{Date: day, Sales: response})
}

// salesOf answers the "which receipts" detail of a report row: up to 500 sales of the period (the last year
// when no start_date is given) behind the row with the id of the path.
func (q *reportController) salesOf(c fiber.Ctx, ctxt, param, kind string) error {
	ctx := c.Context()
	id, err := uuid.Parse(c.Params(param))
	if err != nil {
		err = helper.NotFound("Report")
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParse")
		return err
	}
	request, err := reportFilter(c, ctxt, true)
	if err != nil {
		return err
	}
	if c.Query("start_date") == "" {
		request.StartDate = time.Now().In(helper.Location).AddDate(-1, 0, 0).Format(time.DateOnly)
	}
	response, err := q.reportService.FindSalesOf(ctx, request, kind, id)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSalesOf")
		return err
	}
	return helper.OK(c, models.ReportSales{Sales: response})
}

// V1SalesOfProduct godoc
//
//	@Summary		List the sales of a product
//	@Description	Up to 500 sales of the period with the product; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			product_id	path		string	true	"product id (UUID)"
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=models.ReportSales}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/sales-by-product/{product_id} [get]
func (q *reportController) V1SalesOfProduct(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfProduct", "product_id", models.ReportSalesOfProduct)
}

// V1SalesOfCustomerPoints godoc
//
//	@Summary		List the point sales of a customer
//	@Description	Up to 500 sales of the period on which the customer earned or redeemed points; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			customer_id	path		string	true	"customer id (UUID)"
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=models.ReportSales}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/customer-points/{customer_id} [get]
func (q *reportController) V1SalesOfCustomerPoints(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfCustomerPoints", "customer_id", models.ReportSalesOfCustomerPoints)
}

// V1SalesOfRedemption godoc
//
//	@Summary		List the sales that redeemed points on a product
//	@Description	Up to 500 sales of the period; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			product_id	path		string	true	"product id (UUID)"
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=models.ReportSales}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-redemptions/{product_id} [get]
func (q *reportController) V1SalesOfRedemption(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfRedemption", "product_id", models.ReportSalesOfRedemption)
}

// V1SalesOfEarning godoc
//
//	@Summary		List the sales that earned points on a product
//	@Description	Up to 500 sales of the period; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			product_id	path		string	true	"product id (UUID)"
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=models.ReportSales}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/point-earnings/{product_id} [get]
func (q *reportController) V1SalesOfEarning(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfEarning", "product_id", models.ReportSalesOfEarning)
}

// V1SalesOfPromotion godoc
//
//	@Summary		List the sales of a promotion
//	@Description	Up to 500 sales of the period that used the promotion; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			promotion_id	path		string	true	"promotion id (UUID)"
//	@Param			start_date		query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date		query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200				{object}	helper.Response{data=models.ReportSales}
//	@Failure		401				{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/transactions/{promotion_id} [get]
func (q *reportController) V1SalesOfPromotion(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfPromotion", "promotion_id", models.ReportSalesOfPromotion)
}

// V1SalesOfPromotionProduct godoc
//
//	@Summary		List the promotion sales of a product
//	@Description	Up to 500 sales of the period that sold the product on a promotion; the last year when no start_date is given.
//	@Tags			Reports
//	@Param			product_id	path		string	true	"product id (UUID)"
//	@Param			start_date	query		string	false	"first day of the period (YYYY-MM-DD)"
//	@Param			end_date	query		string	false	"last day of the period (YYYY-MM-DD); today by default"
//	@Success		200			{object}	helper.Response{data=models.ReportSales}
//	@Failure		401			{object}	helper.Response{data=helper.ErrorData}
//	@Router			/reports/promotions/products/{product_id} [get]
func (q *reportController) V1SalesOfPromotionProduct(c fiber.Ctx) error {
	return q.salesOf(c, "ReportController-V1SalesOfPromotionProduct", "product_id", models.ReportSalesOfPromotionItem)
}

// V1StoreRating godoc
//
//	@Summary	Show a store rating
//	@Tags		Reports
//	@Param		id	path		string	true	"rating id (UUID)"
//	@Success	200	{object}	helper.Response{data=models.StoreRatingRow}
//	@Failure	401	{object}	helper.Response{data=helper.ErrorData}
//	@Failure	404	{object}	helper.Response{data=helper.ErrorData}
//	@Router		/reports/store-ratings/{id} [get]
func (q *reportController) V1StoreRating(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "ReportController-V1StoreRating"
	ratingID, err := sanitizers.ParamID(c, "id", "Review")
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrParamID")
		return err
	}
	response, err := q.reportService.FindStoreRating(ctx, currentBranchID(c), ratingID)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreRating")
		if errors.Is(err, models.ErrStoreRatingNotFound) {
			return helper.NotFound("Review")
		}
		return err
	}
	return helper.OK(c, response)
}

type dashboardController struct {
	authService    *middlewares.Service
	reportService  services.ReportService
	settingService services.SettingService
}

func NewDashboardController(
	authService *middlewares.Service,
	reportService services.ReportService,
	settingService services.SettingService,
) *dashboardController {
	return &dashboardController{
		authService:    authService,
		reportService:  reportService,
		settingService: settingService,
	}
}

// Mount registers GET /dashboard.
func (q *dashboardController) Mount(r fiber.Router) {
	r.Get("/dashboard", q.authService.Middleware(), q.V1Show)
}

// V1Show godoc
//
//	@Summary		Show the dashboard
//	@Description	The sales of a day of the active branch next to the day before, the sales per hour, the best sellers, the products low on stock and what needs the owner's attention (store location, subscription, storage).
//	@Tags			Dashboard
//	@Param			date					query		string	false	"day (YYYY-MM-DD); today by default"
//	@Param			include_unpaid_revenue	query		bool	false	"count the unpaid part of credit sales as revenue"
//	@Param			include_unpaid_profit	query		bool	false	"count the profit of unpaid credit sales"
//	@Success		200						{object}	helper.Response{data=models.Dashboard}
//	@Failure		401						{object}	helper.Response{data=helper.ErrorData}
//	@Failure		422						{object}	helper.Response{data=helper.ErrorData}
//	@Router			/dashboard [get]
func (q *dashboardController) V1Show(c fiber.Ctx) error {
	ctx := c.Context()
	ctxt := "DashboardController-V1Show"
	request, err := sanitizers.FindDashboard(c)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDashboard")
		return err
	}
	request.BranchID = currentBranchID(c)
	response, err := q.reportService.FindDashboard(ctx, request)
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindDashboard")
		return err
	}
	alert := func(kind string, message i18n.Text, label, route string) {
		response.Alerts = append(response.Alerts, &models.DashboardAlert{
			Type:        kind,
			Message:     message.In(helper.Lang(c)),
			ActionLabel: helper.T(c, label),
			ActionRoute: route,
		})
	}
	var store models.StoreSetting
	if err = q.settingService.FindSetting(ctx, nil, request.BranchID, models.SettingStore, &store); err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindSetting")
		return err
	}
	if store.Latitude == nil {
		alert("store_location", i18n.Tr("Your store has no map location yet."), "Set location", "store")
	}
	usage, err := q.reportService.FindStoreUsage(ctx, currentStoreID(c))
	if err != nil {
		helper.Log(ctx, slog.LevelError, err.Error(), ctxt, "ErrFindStoreUsage")
		return err
	}
	now := time.Now()
	if expires := usage.PlanExpiresAt; expires != nil && expires.Sub(now) < 7*24*time.Hour {
		message := i18n.Tr("Your subscription ends on {date}.", "date", expires.In(helper.Location).Format("02-01-2006"))
		if expires.Before(now) {
			message = i18n.Tr("Your subscription has ended.")
		}
		alert("subscription", message, "Renew", "billing")
	}
	if usage.StorageQuotaMB > 0 && usage.UsedMB/usage.StorageQuotaMB > 0.9 {
		alert("storage", i18n.Tr("Your storage is almost full."), "Add storage", "storage")
	}
	return helper.OK(c, response)
}

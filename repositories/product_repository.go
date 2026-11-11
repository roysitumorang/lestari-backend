package repositories

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/models"
	"github.com/roysitumorang/lestari-backend/pools"
)

type ProductRepository interface {
	FindProducts(
		ctx context.Context,
		request *models.ProductFilter,
	) ([]*models.Product, int64, error)
	FindProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.ProductFilter,
	) (*models.Product, error)
	CreateProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Product,
	) (*models.Product, error)
	UpdateProduct(
		ctx context.Context,
		tx pgx.Tx,
		request *models.Product,
	) (*models.Product, error)
	DeleteProducts(
		ctx context.Context,
		tx pgx.Tx,
		request *models.ProductFilter,
		deletedAt time.Time,
	) (int64, error)
	// NextSKU is the next free automatic SKU (P000001, P000002 …) of a branch; with a nil tx the read pool is used.
	NextSKU(
		ctx context.Context,
		tx pgx.Tx,
		branchID uuid.UUID,
	) (string, error)
	// SKUExists tells whether another product of the branch (other than exceptProductID) has this SKU.
	SKUExists(
		ctx context.Context,
		branchID uuid.UUID,
		sku string,
		exceptProductID *uuid.UUID,
	) (bool, error)
}

type productRepository struct {
	dbRead,
	dbWrite *pgxpool.Pool
}

func NewProductRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) ProductRepository {
	return &productRepository{
		dbRead:  dbRead,
		dbWrite: dbWrite,
	}
}

// productColumns is the SELECT list of products (alias t), in the order scanProduct reads it.
const productColumns = `t.id
	, t.branch_id
	, t.image_url
	, t.name
	, t.item_type
	, t.sku
	, t.barcode
	, t.category_id
	, (SELECT r.name FROM categories r WHERE r.id = t.category_id)
	, t.unit
	, t.selling_price::float8
	, t.purchase_price::float8
	, t.price_type
	, t.tax_id
	, (SELECT r.name FROM taxes r WHERE r.id = t.tax_id)
	, t.discount_type
	, t.discount_value::float8
	, t.wholesale_prices
	, t.customer_type_prices
	, t.track_stock
	, t.stock::float8
	, t.minimum_stock::float8
	, t.shelf_location
	, t.weight_grams::float8
	, t.track_imei
	, t.units
	, t.variants
	, t.ingredients
	, t.points_earned::float8
	, t.points_to_redeem::float8
	, t.show_in_online_store
	, t.description
	, t.online_description
	, t.created_at
	, t.updated_at`

// productSortings maps the sortable fields to their SQL expressions.
var productSortings = map[string]string{
	"id":                   "t.id",
	"created_at":           "t.created_at",
	"updated_at":           "t.updated_at",
	"image_url":            "t.image_url",
	"name":                 "t.name",
	"item_type":            "t.item_type",
	"sku":                  "t.sku",
	"barcode":              "t.barcode",
	"category_id":          "t.category_id",
	"unit":                 "t.unit",
	"selling_price":        "t.selling_price",
	"purchase_price":       "t.purchase_price",
	"price_type":           "t.price_type",
	"tax_id":               "t.tax_id",
	"discount_type":        "t.discount_type",
	"discount_value":       "t.discount_value",
	"wholesale_prices":     "t.wholesale_prices",
	"customer_type_prices": "t.customer_type_prices",
	"track_stock":          "t.track_stock",
	"stock":                "t.stock",
	"minimum_stock":        "t.minimum_stock",
	"shelf_location":       "t.shelf_location",
	"weight_grams":         "t.weight_grams",
	"track_imei":           "t.track_imei",
	"units":                "t.units",
	"variants":             "t.variants",
	"ingredients":          "t.ingredients",
	"points_earned":        "t.points_earned",
	"points_to_redeem":     "t.points_to_redeem",
	"show_in_online_store": "t.show_in_online_store",
	"description":          "t.description",
	"online_description":   "t.online_description",
	"category.name":        "(SELECT r.name FROM categories r WHERE r.id = t.category_id)",
	"tax.name":             "(SELECT r.name FROM taxes r WHERE r.id = t.tax_id)",
}

func scanProduct(row pgx.Row) (*models.Product, error) {
	var response models.Product
	var categoryName *string
	var taxName *string
	err := row.Scan(
		&response.ID,
		&response.BranchID,
		&response.ImageURL,
		&response.Name,
		&response.ItemType,
		&response.SKU,
		&response.Barcode,
		&response.CategoryID,
		&categoryName,
		&response.Unit,
		&response.SellingPrice,
		&response.PurchasePrice,
		&response.PriceType,
		&response.TaxID,
		&taxName,
		&response.DiscountType,
		&response.DiscountValue,
		&response.WholesalePrices,
		&response.CustomerTypePrices,
		&response.TrackStock,
		&response.Stock,
		&response.MinimumStock,
		&response.ShelfLocation,
		&response.WeightGrams,
		&response.TrackIMEI,
		&response.Units,
		&response.Variants,
		&response.Ingredients,
		&response.PointsEarned,
		&response.PointsToRedeem,
		&response.ShowInOnlineStore,
		&response.Description,
		&response.OnlineDescription,
		&response.CreatedAt,
		&response.UpdatedAt,
	)
	response.Category = models.NewReference(response.CategoryID, categoryName)
	response.Tax = models.NewReference(response.TaxID, taxName)
	return &response, err
}

func (q *productRepository) FindProducts(
	ctx context.Context,
	request *models.ProductFilter,
) ([]*models.Product, int64, error) {
	ctxt := "ProductRepository-FindProducts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT COUNT(1) FROM products t %s", condition)
	var total int64
	err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&total)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, 0, err
	}
	response := []*models.Product{}
	if total == 0 {
		return response, 0, nil
	}
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s ORDER BY ", productColumns, condition)
	if len(request.Sortings) == 0 {
		_, _ = sb.WriteString("t.id DESC")
	}
	for i, item := range request.Sortings {
		if i > 0 {
			_, _ = sb.WriteString(", ")
		}
		// the expression comes from productSortings, never from the request text
		_, _ = fmt.Fprintf(sb, "%s %s NULLS LAST, t.id %s", productSortings[item.Field], item.Direction, item.Direction)
	}
	if request.Limit > 0 {
		_, _ = fmt.Fprintf(sb, " LIMIT %d OFFSET %d", request.Limit, max(request.Page-1, 0)*request.Limit)
	}
	rows, err := q.dbRead.Query(ctx, sb.String(), params...)
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrQuery")
		return nil, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		product, err := scanProduct(rows)
		if err != nil {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
			return nil, 0, err
		}
		response = append(response, product)
	}
	if err = rows.Err(); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrRows")
		return nil, 0, err
	}
	return response, total, nil
}

// FindProduct returns the first product of the filter. Inside a transaction pass tx so the read sees its writes; with a
// nil tx the read pool is used.
func (q *productRepository) FindProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.ProductFilter,
) (*models.Product, error) {
	ctxt := "ProductRepository-FindProduct"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	sb.Reset()
	_, _ = fmt.Fprintf(sb, "SELECT %s FROM products t %s LIMIT 1", productColumns, condition)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), params...)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), params...)
	}
	response, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	return response, nil
}

func (q *productRepository) CreateProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Product,
) (*models.Product, error) {
	ctxt := "ProductRepository-CreateProduct"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`INSERT INTO products (
		branch_id
		, image_url
		, name
		, item_type
		, sku
		, barcode
		, category_id
		, unit
		, selling_price
		, purchase_price
		, price_type
		, tax_id
		, discount_type
		, discount_value
		, wholesale_prices
		, customer_type_prices
		, track_stock
		, stock
		, minimum_stock
		, shelf_location
		, weight_grams
		, track_imei
		, units
		, variants
		, ingredients
		, points_earned
		, points_to_redeem
		, show_in_online_store
		, description
		, online_description
		, created_at
		, updated_at
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15::jsonb, $16::jsonb, $17, $18, $19, $20, $21, $22, $23::jsonb, $24::jsonb, $25::jsonb, $26, $27, $28, $29, $30, $31, $32)
	RETURNING id`)
	var id uuid.UUID
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.BranchID,
		request.ImageURL,
		request.Name,
		request.ItemType,
		request.SKU,
		request.Barcode,
		request.CategoryID,
		request.Unit,
		request.SellingPrice,
		request.PurchasePrice,
		request.PriceType,
		request.TaxID,
		request.DiscountType,
		request.DiscountValue,
		request.WholesalePrices,
		request.CustomerTypePrices,
		request.TrackStock,
		request.Stock,
		request.MinimumStock,
		request.ShelfLocation,
		request.WeightGrams,
		request.TrackIMEI,
		request.Units,
		request.Variants,
		request.Ingredients,
		request.PointsEarned,
		request.PointsToRedeem,
		request.ShowInOnlineStore,
		request.Description,
		request.OnlineDescription,
		request.CreatedAt,
		request.UpdatedAt,
	).Scan(&id)
	if err != nil {
		if err = writeError(err, models.ErrProductConflict); !errors.Is(err, models.ErrProductConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
			helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		}
		return nil, err
	}
	return q.FindProduct(ctx, tx, models.NewProductFilter().WithProductIDs(id))
}

// UpdateProduct locks the row and writes only the attributes whose value differs from the stored one. When nothing
// changed no UPDATE is executed; otherwise updated_at is set to request.UpdatedAt (the caller's time.Now()).
func (q *productRepository) UpdateProduct(
	ctx context.Context,
	tx pgx.Tx,
	request *models.Product,
) (*models.Product, error) {
	ctxt := "ProductRepository-UpdateProduct"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT t.image_url
		, t.name
		, t.item_type
		, t.sku
		, t.barcode
		, t.category_id
		, t.unit
		, t.selling_price::float8
		, t.purchase_price::float8
		, t.price_type
		, t.tax_id
		, t.discount_type
		, t.discount_value::float8
		, t.wholesale_prices
		, t.customer_type_prices
		, t.track_stock
		, t.stock::float8
		, t.minimum_stock::float8
		, t.shelf_location
		, t.weight_grams::float8
		, t.track_imei
		, t.units
		, t.variants
		, t.ingredients
		, t.points_earned::float8
		, t.points_to_redeem::float8
		, t.show_in_online_store
		, t.description
		, t.online_description
	FROM products t
	WHERE t.id = $1
		AND t.branch_id = $2
		AND t.deleted_at IS NULL
	FOR NO KEY UPDATE`)
	var current models.Product
	err := tx.QueryRow(
		ctx,
		sb.String(),
		request.ID,
		request.BranchID,
	).Scan(
		&current.ImageURL,
		&current.Name,
		&current.ItemType,
		&current.SKU,
		&current.Barcode,
		&current.CategoryID,
		&current.Unit,
		&current.SellingPrice,
		&current.PurchasePrice,
		&current.PriceType,
		&current.TaxID,
		&current.DiscountType,
		&current.DiscountValue,
		&current.WholesalePrices,
		&current.CustomerTypePrices,
		&current.TrackStock,
		&current.Stock,
		&current.MinimumStock,
		&current.ShelfLocation,
		&current.WeightGrams,
		&current.TrackIMEI,
		&current.Units,
		&current.Variants,
		&current.Ingredients,
		&current.PointsEarned,
		&current.PointsToRedeem,
		&current.ShowInOnlineStore,
		&current.Description,
		&current.OnlineDescription,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, models.ErrProductNotFound
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return nil, err
	}
	var params []any
	var n int
	sb.Reset()
	_, _ = sb.WriteString("UPDATE products SET")
	if !equalPtr(current.ImageURL, request.ImageURL) {
		params = append(params, request.ImageURL)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " image_url = $%d", n)
	}
	if current.Name != request.Name {
		params = append(params, request.Name)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " name = $%d", n)
	}
	if current.ItemType != request.ItemType {
		params = append(params, request.ItemType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " item_type = $%d", n)
	}
	if current.SKU != request.SKU {
		params = append(params, request.SKU)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " sku = $%d", n)
	}
	if !equalPtr(current.Barcode, request.Barcode) {
		params = append(params, request.Barcode)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " barcode = $%d", n)
	}
	if !equalPtr(current.CategoryID, request.CategoryID) {
		params = append(params, request.CategoryID)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " category_id = $%d", n)
	}
	if !equalPtr(current.Unit, request.Unit) {
		params = append(params, request.Unit)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " unit = $%d", n)
	}
	if current.SellingPrice != request.SellingPrice {
		params = append(params, request.SellingPrice)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " selling_price = $%d", n)
	}
	if current.PurchasePrice != request.PurchasePrice {
		params = append(params, request.PurchasePrice)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " purchase_price = $%d", n)
	}
	if current.PriceType != request.PriceType {
		params = append(params, request.PriceType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " price_type = $%d", n)
	}
	if !equalPtr(current.TaxID, request.TaxID) {
		params = append(params, request.TaxID)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " tax_id = $%d", n)
	}
	if !equalPtr(current.DiscountType, request.DiscountType) {
		params = append(params, request.DiscountType)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " discount_type = $%d", n)
	}
	if !equalPtr(current.DiscountValue, request.DiscountValue) {
		params = append(params, request.DiscountValue)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " discount_value = $%d", n)
	}
	if !equalJSON(current.WholesalePrices, request.WholesalePrices) {
		params = append(params, request.WholesalePrices)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " wholesale_prices = $%d::jsonb", n)
	}
	if !equalJSON(current.CustomerTypePrices, request.CustomerTypePrices) {
		params = append(params, request.CustomerTypePrices)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " customer_type_prices = $%d::jsonb", n)
	}
	if current.TrackStock != request.TrackStock {
		params = append(params, request.TrackStock)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " track_stock = $%d", n)
	}
	if !equalPtr(current.MinimumStock, request.MinimumStock) {
		params = append(params, request.MinimumStock)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " minimum_stock = $%d", n)
	}
	if !equalPtr(current.ShelfLocation, request.ShelfLocation) {
		params = append(params, request.ShelfLocation)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " shelf_location = $%d", n)
	}
	if !equalPtr(current.WeightGrams, request.WeightGrams) {
		params = append(params, request.WeightGrams)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " weight_grams = $%d", n)
	}
	if current.TrackIMEI != request.TrackIMEI {
		params = append(params, request.TrackIMEI)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " track_imei = $%d", n)
	}
	if !equalJSON(current.Units, request.Units) {
		params = append(params, request.Units)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " units = $%d::jsonb", n)
	}
	if !equalJSON(current.Variants, request.Variants) {
		params = append(params, request.Variants)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " variants = $%d::jsonb", n)
	}
	if !equalJSON(current.Ingredients, request.Ingredients) {
		params = append(params, request.Ingredients)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " ingredients = $%d::jsonb", n)
	}
	if !equalPtr(current.PointsEarned, request.PointsEarned) {
		params = append(params, request.PointsEarned)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " points_earned = $%d", n)
	}
	if !equalPtr(current.PointsToRedeem, request.PointsToRedeem) {
		params = append(params, request.PointsToRedeem)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " points_to_redeem = $%d", n)
	}
	if current.ShowInOnlineStore != request.ShowInOnlineStore {
		params = append(params, request.ShowInOnlineStore)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " show_in_online_store = $%d", n)
	}
	if !equalPtr(current.Description, request.Description) {
		params = append(params, request.Description)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " description = $%d", n)
	}
	if !equalPtr(current.OnlineDescription, request.OnlineDescription) {
		params = append(params, request.OnlineDescription)
		if n = len(params); n > 1 {
			_, _ = sb.WriteString(",")
		}
		_, _ = fmt.Fprintf(sb, " online_description = $%d", n)
	}
	if len(params) > 0 {
		params = append(params, request.UpdatedAt)
		_, _ = fmt.Fprintf(sb, ", updated_at = $%d", len(params))
		params = append(params, request.ID)
		_, _ = fmt.Fprintf(sb, " WHERE id = $%d", len(params))
		if _, err = tx.Exec(ctx, sb.String(), params...); err != nil {
			if err = writeError(err, models.ErrProductConflict); !errors.Is(err, models.ErrProductConflict) && !errors.Is(err, models.ErrReferenceNotFound) {
				helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
			}
			return nil, err
		}
	}
	return q.FindProduct(ctx, tx, models.NewProductFilter().WithProductIDs(request.ID))
}

// DeleteProducts marks as deleted (deleted_at, the caller's time.Now()) the products of the filter and returns how many went.
func (q *productRepository) DeleteProducts(
	ctx context.Context,
	tx pgx.Tx,
	request *models.ProductFilter,
	deletedAt time.Time,
) (int64, error) {
	ctxt := "ProductRepository-DeleteProducts"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	condition, params := q.buildCondition(request, sb)
	if len(request.ProductIDs) == 0 {
		return 0, errors.New("product: refusing to delete without a condition")
	}
	sb.Reset()
	params = append(params, deletedAt)
	_, _ = fmt.Fprintf(sb, "UPDATE products t SET deleted_at = $%d, updated_at = $%d %s", len(params), len(params), condition)
	tag, err := tx.Exec(ctx, sb.String(), params...)
	if IsForeignKeyViolation(err) {
		return 0, models.ErrProductInUse
	}
	if err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrExec")
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (q *productRepository) buildCondition(request *models.ProductFilter, sb *strings.Builder) (condition string, params []any) {
	conds := []string{"t.deleted_at IS NULL"}
	if len(request.ProductIDs) > 0 {
		params = append(params, request.ProductIDs)
		conds = append(conds, helper.Sprintf("t.id = ANY($%d)", len(params)))
	}
	if len(request.BranchIDs) > 0 {
		params = append(params, request.BranchIDs)
		conds = append(conds, helper.Sprintf("t.branch_id = ANY($%d)", len(params)))
	}
	if request.ItemType != nil {
		params = append(params, *request.ItemType)
		conds = append(conds, helper.Sprintf("t.item_type = $%d", len(params)))
	}
	if request.CategoryID != nil {
		params = append(params, *request.CategoryID)
		conds = append(conds, helper.Sprintf("t.category_id = $%d", len(params)))
	}
	if request.StockStatus != "" {
		params = append(params, request.StockStatus)
		conds = append(conds, helper.Sprintf("(CASE $%d WHEN 'in_stock' THEN t.stock > 0 WHEN 'low' THEN t.minimum_stock IS NOT NULL AND t.stock <= t.minimum_stock AND t.stock > 0 WHEN 'out' THEN t.stock <= 0 ELSE true END)", len(params)))
	}
	if request.SKU != "" {
		params = append(params, request.SKU)
		conds = append(conds, helper.Sprintf("LOWER(t.sku) = LOWER($%d)", len(params)))
	}
	if request.Keyword != "" {
		params = append(params, "%"+request.Keyword+"%")
		n := len(params)
		conds = append(conds, helper.Sprintf("((t.name)::text ILIKE $%d OR (t.sku)::text ILIKE $%d OR (t.barcode)::text ILIKE $%d)", n, n, n))
	}
	if len(conds) > 0 {
		sb.Reset()
		_, _ = sb.WriteString("WHERE ")
		_, _ = sb.WriteString(strings.Join(conds, " AND "))
		condition = sb.String()
	}
	return
}

func (q *productRepository) NextSKU(
	ctx context.Context,
	tx pgx.Tx,
	branchID uuid.UUID,
) (string, error) {
	ctxt := "ProductRepository-NextSKU"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT COALESCE(MAX(NULLIF(REGEXP_REPLACE(t.sku, '^P', ''), '')::bigint), 0) + 1
	FROM products t
	WHERE t.branch_id = $1
		AND t.sku ~ '^P[0-9]{1,15}$'`)
	var row pgx.Row
	if tx != nil {
		row = tx.QueryRow(ctx, sb.String(), branchID)
	} else {
		row = q.dbRead.QueryRow(ctx, sb.String(), branchID)
	}
	var next int64
	if err := row.Scan(&next); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return "", err
	}
	return helper.Sprintf("P%06d", next), nil
}

func (q *productRepository) SKUExists(
	ctx context.Context,
	branchID uuid.UUID,
	sku string,
	exceptProductID *uuid.UUID,
) (bool, error) {
	ctxt := "ProductRepository-SKUExists"
	sb := pools.BuilderPool.Get()
	defer pools.BuilderPool.Put(sb)
	sb.Reset()
	_, _ = sb.WriteString(`SELECT EXISTS(
		SELECT 1
		FROM products t
		WHERE t.branch_id = $1
			AND LOWER(t.sku) = LOWER($2)
			AND t.deleted_at IS NULL`)
	params := []any{branchID, sku}
	if exceptProductID != nil {
		params = append(params, *exceptProductID)
		_, _ = fmt.Fprintf(sb, " AND t.id <> $%d", len(params))
	}
	_, _ = sb.WriteString(")")
	var exists bool
	if err := q.dbRead.QueryRow(ctx, sb.String(), params...).Scan(&exists); err != nil {
		helper.Capture(ctx, slog.LevelError, err, ctxt, "ErrScan")
		return false, err
	}
	return exists, nil
}

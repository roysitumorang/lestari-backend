package api

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/roysitumorang/lestari-backend/helper"
	"github.com/roysitumorang/lestari-backend/repositories"
)

// productStock adjusts stock and writes a movement; it is the single place stock changes.
func productStock(ctx context.Context, tx pgx.Tx, branchID, productID string, change float64, typ, ref string, unitCost *float64, actorID string, actor, note string) (float64, error) {
	var after float64
	var track bool
	err := tx.QueryRow(ctx, `UPDATE products SET stock = stock + $3, updated_at = now() WHERE id = $1 AND branch_id = $2 RETURNING stock, track_stock`,
		productID, branchID, change).Scan(&after, &track)
	if err != nil {
		if repositories.IsNoRows(err) {
			return 0, helper.Invalid("product_id", "Product {id} not found.", "id", productID)
		}
		return 0, err
	}
	if !track {
		return after, nil
	}
	var actorRef any
	if actorID != "" {
		actorRef = actorID
	}
	_, err = tx.Exec(ctx, `INSERT INTO stock_movements (branch_id, product_id, type, reference, quantity_change, stock_after, unit_cost, actor_id, actor_name, note)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,NULLIF($10,''))`, branchID, productID, typ, ref, change, after, unitCost, actorRef, actor, note)
	return after, err
}

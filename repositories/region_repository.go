package repositories

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

type RegionRepository interface {
	FindProvinces(ctx context.Context, countryCode, keyword string) ([]*models.Region, error)
	// FindCities lists the cities of a province (nil: of every province).
	FindCities(ctx context.Context, provinceID *uuid.UUID, keyword string) ([]*models.Region, error)
}

type regionRepository struct {
	base
}

func NewRegionRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) RegionRepository {
	return &regionRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *regionRepository) FindProvinces(ctx context.Context, countryCode, keyword string) ([]*models.Region, error) {
	ctxt := "RegionRepository-FindProvinces"
	return queryRows[models.Region](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.name
	FROM provinces t
	WHERE t.country_code = $1
		AND ($2 = '' OR t.name ILIKE '%' || $2 || '%')
	ORDER BY t.name`, countryCode, keyword)
}

func (q *regionRepository) FindCities(ctx context.Context, provinceID *uuid.UUID, keyword string) ([]*models.Region, error) {
	ctxt := "RegionRepository-FindCities"
	return queryRows[models.Region](ctx, q.dbRead, ctxt, `SELECT t.id
		, t.name
		, t.province_id
	FROM cities t
	WHERE ($1::uuid IS NULL OR t.province_id = $1::uuid)
		AND ($2 = '' OR t.name ILIKE '%' || $2 || '%')
	ORDER BY t.name`, provinceID, keyword)
}

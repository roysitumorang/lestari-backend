package repositories

import (
	"context"
	"uuid"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/roysitumorang/lestari-backend/models"
)

type UploadRepository interface {
	CreateUpload(ctx context.Context, request *models.Upload) (uuid.UUID, error)
}

type uploadRepository struct {
	base
}

func NewUploadRepository(
	dbRead,
	dbWrite *pgxpool.Pool,
) UploadRepository {
	return &uploadRepository{base{dbRead: dbRead, dbWrite: dbWrite}}
}

func (q *uploadRepository) CreateUpload(ctx context.Context, request *models.Upload) (uuid.UUID, error) {
	ctxt := "UploadRepository-CreateUpload"
	return queryValue[uuid.UUID](ctx, q.dbWrite, ctxt, `INSERT INTO uploads (
		store_id
		, path
		, url
		, mime_type
		, size
		, purpose
	) VALUES ($1, $2, $3, $4, $5, $6)
	RETURNING id`, request.StoreID, request.Path, request.URL, request.MimeType, request.Size, request.Purpose)
}

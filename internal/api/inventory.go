package api

import (
	"context"
	"slices"
	"time"

	"github.com/goccy/go-json"
)

// startJob records a background job and runs fn in a goroutine. It returns the job id.
func (a *API) startJob(storeID string, kind string, payload any, fn func(ctx context.Context) (any, error)) (string, error) {
	var id string
	pb, _ := json.Marshal(payload)
	if err := a.DB.QueryRow(context.Background(), `INSERT INTO jobs (store_id, kind, status, payload) VALUES ($1,$2,'queued',$3::jsonb) RETURNING id`, storeID, kind, string(pb)).Scan(&id); err != nil {
		return "", err
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='running' WHERE id=$1`, id)
		res, err := fn(ctx)
		if err != nil {
			a.Log.Error("job failed", "job", id, "kind", kind, "err", err)
			_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='failed', error=$2, finished_at=now() WHERE id=$1`, id, err.Error())
			return
		}
		rb, _ := json.Marshal(res)
		_, _ = a.DB.Exec(ctx, `UPDATE jobs SET status='completed', result=$2::jsonb, finished_at=now() WHERE id=$1`, id, string(rb))
	}()
	return id, nil
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

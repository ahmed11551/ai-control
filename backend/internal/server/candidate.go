package server

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// candidateQuerier accepts both a pool and a transaction. Callers already inside
// a transaction must pass that transaction, including with a one-connection pool.
type candidateQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// The trigger assigns identity in the same statement and locks the lead row.
// A missing lead or failed assignment is an error, never an empty success.
func ensureCandidateForLead(ctx context.Context, db candidateQuerier, leadID string) (string, error) {
	var id string
	err := db.QueryRow(ctx, `
		UPDATE leads SET candidate_id=candidate_id WHERE id=$1::uuid
		RETURNING candidate_id::text`, leadID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("ensure candidate for lead %s: %w", leadID, err)
	}
	return id, nil
}

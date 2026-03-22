package testutils

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Nesquiko/servermore/pkg/commander"
	testqueries "github.com/Nesquiko/servermore/test/test_utils/queries.gen"
	"github.com/stretchr/testify/require"
)

type testQueries struct {
	queries *testqueries.Queries
}

var _ testqueries.Querier = (*testQueries)(nil)

func (r *testQueries) FunctionById(ctx context.Context, id int64) (testqueries.Function, error) {
	return commander.WithRetry(ctx, func(ctx context.Context) (testqueries.Function, error) {
		return r.queries.FunctionById(ctx, id)
	})
}

func TestDB(t *testing.T, dbPath string) testqueries.Querier {
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	_, err = commander.WithRetry(t.Context(), func(ctx context.Context) (struct{}, error) {
		return struct{}{}, db.PingContext(ctx)
	})
	require.NoError(t, err)

	return &testQueries{queries: testqueries.New(db)}
}

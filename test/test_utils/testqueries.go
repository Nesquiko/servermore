package testutils

import (
	"context"
	"database/sql"

	"github.com/Nesquiko/servermore/pkg/commander"
	testqueries "github.com/Nesquiko/servermore/test/test_utils/queries.gen"
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

func (r *testQueries) RunnerByAddr(ctx context.Context, addr string) (testqueries.Runner, error) {
	return commander.WithRetry(ctx, func(ctx context.Context) (testqueries.Runner, error) {
		return r.queries.RunnerByAddr(ctx, addr)
	})
}

func OpenTestDB(ctx context.Context, dbPath string) (testqueries.Querier, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	_, err = commander.WithRetry(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, db.PingContext(ctx)
	})
	if err != nil {
		return nil, err
	}

	return &testQueries{queries: testqueries.New(db)}, nil
}

package commander

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Nesquiko/servermore/pkg/queries.gen"
	_ "modernc.org/sqlite"
)

type CommanderDB interface {
	CreateFunction(ctx context.Context, path string, name string) (queries.Function, error)
	FunctionExistsByHash(ctx context.Context, hash []byte) (bool, error)
}

type SQLiteCommanderDB struct {
	queries *queries.Queries
}

func NewSQLiteDB(dbUri string) (*SQLiteCommanderDB, error) {
	db, err := sql.Open("sqlite", dbUri)
	if err != nil {
		return nil, err
	}

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	return &SQLiteCommanderDB{queries: queries.New(db)}, nil
}

var _ CommanderDB = (*SQLiteCommanderDB)(nil)

// CreateFunction implements [CommanderDB].
func (s *SQLiteCommanderDB) CreateFunction(
	ctx context.Context,
	path string,
	name string,
) (queries.Function, error) {
	f, err := s.queries.CreateFunction(ctx, queries.CreateFunctionParams{Path: path, Name: name})
	if err != nil {
		return queries.Function{}, fmt.Errorf("failed to create function: %w", err)
	}

	return f, nil
}

// FunctionExistsByHash implements [CommanderDB].
func (s *SQLiteCommanderDB) FunctionExistsByHash(ctx context.Context, hash []byte) (bool, error) {
	exists, err := s.queries.FunctionExistsByHash(ctx, hash)
	if err != nil {
		return false, fmt.Errorf("failed to check if function exists: %w", err)
	}

	return exists == 1, nil
}

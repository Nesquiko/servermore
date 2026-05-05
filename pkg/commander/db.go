package commander

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/Nesquiko/servermore/pkg/commander/queries.gen"
	"github.com/golang-migrate/migrate/v4"
	sqlitemigrate "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	sqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

//go:embed migrations/*.sql
var migrations embed.FS

type CommanderDB interface {
	io.Closer

	CreateFunction(
		ctx context.Context,
		path string,
		name string,
		hash []byte,
	) (queries.Function, error)
	FunctionByID(ctx context.Context, id int64) (queries.Function, error)
	FunctionExistsByHash(ctx context.Context, hash []byte) (bool, error)
	CreateRunner(ctx context.Context, addr string) (queries.Runner, error)
	RunnerByAddr(ctx context.Context, addr string) (queries.Runner, error)
	GetAllRunners(ctx context.Context) ([]queries.Runner, error)
}

type SQLiteCommanderDB struct {
	db      *sql.DB
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

	if err = migrateUp(db); err != nil {
		return nil, fmt.Errorf("migrate db schema failed: %w", err)
	}

	return &SQLiteCommanderDB{db: db, queries: queries.New(db)}, nil
}

func migrateUp(db *sql.DB) error {
	source, err := iofs.New(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations: %w", err)
	}

	driver, err := sqlitemigrate.WithInstance(db, &sqlitemigrate.Config{})
	if err != nil {
		return fmt.Errorf("failed to create sqlite migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", source, "sqlite", driver)
	if err != nil {
		return fmt.Errorf("failed to initialize migrate: %w", err)
	}
	m.Log = slogLogger{verbose: true}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up failed: %w", err)
	}

	return nil
}

var _ CommanderDB = (*SQLiteCommanderDB)(nil)

// CreateFunction implements [CommanderDB].
func (s *SQLiteCommanderDB) CreateFunction(
	ctx context.Context,
	path string,
	name string,
	hash []byte,
) (queries.Function, error) {
	f, err := WithRetry(ctx, func(ctx context.Context) (queries.Function, error) {
		return s.queries.CreateFunction(
			ctx,
			queries.CreateFunctionParams{Path: path, Name: name, Hash: hash},
		)
	})
	if err != nil {
		return queries.Function{}, fmt.Errorf("failed to create function: %w", err)
	}

	return f, nil
}

// FunctionByID implements [CommanderDB].
func (s *SQLiteCommanderDB) FunctionByID(ctx context.Context, id int64) (queries.Function, error) {
	f, err := WithRetry(ctx, func(ctx context.Context) (queries.Function, error) {
		return s.queries.FunctionByID(ctx, id)
	})
	if err != nil {
		return queries.Function{}, fmt.Errorf("failed to get function by id: %w", err)
	}

	return f, nil
}

// FunctionExistsByHash implements [CommanderDB].
func (s *SQLiteCommanderDB) FunctionExistsByHash(ctx context.Context, hash []byte) (bool, error) {
	exists, err := WithRetry(ctx, func(ctx context.Context) (int64, error) {
		return s.queries.FunctionExistsByHash(ctx, hash)
	})
	if err != nil {
		return false, fmt.Errorf("failed to check if function exists: %w", err)
	}

	return exists == 1, nil
}

// CreateRunner implements [CommanderDB].
func (s *SQLiteCommanderDB) CreateRunner(ctx context.Context, addr string) (queries.Runner, error) {
	runner, err := WithRetry(ctx, func(ctx context.Context) (queries.Runner, error) {
		return s.queries.CreateRunner(ctx, addr)
	})
	if err != nil {
		return queries.Runner{}, fmt.Errorf("failed create runner by addr: %w", err)
	}
	return runner, nil
}

// RunnerByAddr implements [CommanderDB].
func (s *SQLiteCommanderDB) RunnerByAddr(ctx context.Context, addr string) (queries.Runner, error) {
	runner, err := WithRetry(ctx, func(ctx context.Context) (queries.Runner, error) {
		return s.queries.RunnerByAddr(ctx, addr)
	})
	if err != nil {
		return queries.Runner{}, fmt.Errorf("failed to get runner by addr: %w", err)
	}
	return runner, nil
}

// GetAllRunners implements [CommanderDB].
func (s *SQLiteCommanderDB) GetAllRunners(ctx context.Context) ([]queries.Runner, error) {
	runners, err := WithRetry(ctx, func(ctx context.Context) ([]queries.Runner, error) {
		return s.queries.GetAllRunners(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get all runners: %w", err)
	}
	return runners, nil
}

// Close implements [CommanderDB].
func (s *SQLiteCommanderDB) Close() error {
	return s.db.Close()
}

const MaxRetries = 20

func WithRetry[R any](ctx context.Context, f func(context.Context) (R, error)) (R, error) {
	retries := 1
	for {
		result, err := f(ctx)
		if nil == err {
			return result, nil
		}

		var sqlErr *sqlite.Error
		if !errors.As(err, &sqlErr) {
			return result, err
		}

		code := sqlErr.Code()
		if code != sqlite3.SQLITE_BUSY && code != sqlite3.SQLITE_LOCKED {
			return result, err
		}

		if retries >= MaxRetries {
			return result, fmt.Errorf("db function errored more than %d times: %w", MaxRetries, err)
		}
		retries++

		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(time.Duration(retries) * 5 * time.Millisecond):
		}
	}
}

// simple wrapper around slog which adheres to migrate.Logger interface
type slogLogger struct {
	verbose bool
}

func (l slogLogger) Printf(format string, v ...any) {
	format = strings.TrimRight(format, "\n")
	msg := fmt.Sprintf(format, v...)
	slog.Info(msg)
}

func (l slogLogger) Verbose() bool {
	return l.verbose
}

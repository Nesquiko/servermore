package commander

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	queries "github.com/Nesquiko/servermore/pkg/commander/queries.gen"
)

type CommanderService struct {
	db          CommanderDB
	funcStorage *FileSystemFunctionStorage
}

var (
	ErrFunctionExists   = errors.New("function with same hash already exists")
	ErrFunctionNotFound = errors.New("function not found")
)

func NewCommanderService(db CommanderDB, funcStorage *FileSystemFunctionStorage) *CommanderService {
	return &CommanderService{db: db, funcStorage: funcStorage}
}

func (svc *CommanderService) CreateFunction(
	ctx context.Context,
	funcName string,
	funcBytesReader io.ReadCloser,
) (api.Function, error) {
	funcBytes, err := io.ReadAll(funcBytesReader)
	if err != nil {
		return api.Function{}, fmt.Errorf("reading function bytes failed: %w", err)
	}
	defer funcBytesReader.Close()

	hash := BytesSha256(funcBytes)

	exists, err := svc.db.FunctionExistsByHash(ctx, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("check for function with hash '%X' failed: %w", hash, err)
	}

	if exists {
		return api.Function{}, ErrFunctionExists
	}

	funcPath, err := svc.funcStorage.Save(funcName, hash, funcBytes)
	if err != nil {
		return api.Function{}, err
	}

	newFunc, err := svc.db.CreateFunction(ctx, funcPath, funcName, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("persisting new function failed: %w", err)
	}

	return api.Function{Id: newFunc.ID, Name: newFunc.Name}, nil
}

func (svc *CommanderService) FunctionByID(ctx context.Context, id int64) (queries.Function, error) {
	function, err := svc.db.FunctionByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return queries.Function{}, ErrFunctionNotFound
	}
	if err != nil {
		return queries.Function{}, fmt.Errorf("querying function by id failed: %w", err)
	}

	return function, nil
}

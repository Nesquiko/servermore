package commander

import (
	"context"
	"errors"
	"fmt"
	"io"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
)

type CommanderService struct {
	db          CommanderDB
	funcStorage *FileSystemFunctionStorage
}

var ErrFunctionExists = errors.New("function with same hash already exists")

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
		return api.Function{}, fmt.Errorf("reading function bytes failed: %v", err)
	}
	defer funcBytesReader.Close()

	hash := BytesSha256(funcBytes)

	exists, err := svc.db.FunctionExistsByHash(ctx, hash)
	if err != nil {
		return api.Function{}, fmt.Errorf("check for function with hash '%X' failed: %v", hash, err)
	}

	if exists {
		return api.Function{}, ErrFunctionExists
	}

	funcPath, err := svc.funcStorage.Save(funcName, hash, funcBytes)
	if err != nil {
		return api.Function{}, err
	}

	newFunc, err := svc.db.CreateFunction(ctx, funcPath, funcName)
	if err != nil {
		return api.Function{}, fmt.Errorf("persisting new function failed: %v", err)
	}

	return api.Function{Id: newFunc.ID, Name: newFunc.Name}, nil
}

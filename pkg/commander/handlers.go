package commander

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	"github.com/Nesquiko/servermore/pkg/server"
)

type CommanderHTTPServerConfig struct {
	AppName    string
	CommitHash string
	Env        string

	Host            string
	Port            string
	BaseURL         string
	DbURI           string
	FuncStorageRoot AbsolutePath
}

type CommanderHTTPServer struct {
	service *CommanderService
}

// Typechecks if CommanderHTTPServer conforms to the interface
var _ api.ServerInterface = (*CommanderHTTPServer)(nil)

func NewCommanderServer(conf CommanderHTTPServerConfig) (*CommanderHTTPServer, error) {
	funcStorage, err := NewFSFunctionStorage(conf.FuncStorageRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize function storage: %v", err)
	}

	db, err := NewSQLiteDB(conf.DbURI)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize db: %w", err)
	}
	svc := NewCommanderService(db, funcStorage)

	return &CommanderHTTPServer{service: svc}, nil
}

// CreateFunction implements [api.ServerInterface].
func (c *CommanderHTTPServer) CreateFunction(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, int64(server.MaxBytes))

	funcName, funcBytesReader, err := parseCreateFunctionMultipartRequest(r)
	if err != nil {
		if err.Error() == server.LargeBodyErrorStr {
			server.EncodeError(w, r, server.Error{
				Cause: err,
				Code:  "function.too.large",
				Detail: fmt.Sprintf(
					"Function binary must have size less than %d bytes",
					server.MaxBytes,
				),
				Status: http.StatusBadRequest,
				Title:  "Function binary too large",
			})
			return
		}
		server.EncodeError(w, r, server.Error{
			Cause:  err,
			Code:   server.InvalidRequestCode,
			Detail: err.Error(),
			Status: http.StatusBadRequest,
			Title:  server.InvalidRequestTitle,
		})
		return
	}

	newFunc, err := c.service.CreateFunction(r.Context(), funcName, funcBytesReader)
	if errors.Is(err, ErrFunctionExists) {
		server.EncodeError(w, r, server.Error{
			Cause:  err,
			Code:   "function.exists",
			Detail: "Function with same bytes already exists",
			Status: http.StatusConflict,
			Title:  "Function already exists",
		})
		return
	} else if err != nil {
		server.InternalServerError(w, r, err)
		return
	}

	server.EncodeResponse(w, r, http.StatusCreated, newFunc)
}

func parseCreateFunctionMultipartRequest(r *http.Request) (string, io.ReadCloser, error) {
	err := r.ParseMultipartForm(server.MaxBytes)
	if err != nil {
		return "", nil, err
	}

	name := r.FormValue("name")

	file, _, err := r.FormFile("binary")
	if err != nil {
		return "", nil, err
	}

	return name, file, nil
}

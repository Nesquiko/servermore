package commander

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	api "github.com/Nesquiko/servermore/pkg/api/commander"
	commonapi "github.com/Nesquiko/servermore/pkg/api/common"
	"github.com/Nesquiko/servermore/pkg/server"
)

const (
	DownloadHeaderFunctionID       = "Function-Id"
	DownloadHeaderFunctionFilename = "Function-Filename"
	DownloadHeaderFunctionPath     = "Function-Path"
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
		return nil, fmt.Errorf("failed to initialize function storage: %w", err)
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

// DownloadFunctionBinary implements [api.ServerInterface].
func (c *CommanderHTTPServer) DownloadFunctionBinary(
	w http.ResponseWriter,
	r *http.Request,
	id string,
) {
	funcId, err := strconv.ParseInt(id, 10, 0)
	if err != nil {
		server.EncodeError(
			w,
			r,
			server.FromInvalidParamErr(
				&commonapi.InvalidParamFormatError{ParamName: "id", Err: err},
			),
		)
		return
	}

	function, err := c.service.FunctionByID(r.Context(), funcId)
	if errors.Is(err, ErrFunctionNotFound) {
		server.EncodeError(w, r, server.Error{
			Cause:  err,
			Code:   "function.not.found",
			Detail: fmt.Sprintf("Function with id '%d' not found", funcId),
			Status: http.StatusNotFound,
			Title:  "Function not found",
		})
		return
	} else if err != nil {
		server.InternalServerError(w, r, err)
		return
	}

	file, err := os.Open(function.Path)
	if err != nil {
		server.InternalServerError(w, r, fmt.Errorf("open function binary: %w", err))
		return
	}
	defer file.Close()

	w.Header().Set(DownloadHeaderFunctionID, strconv.FormatInt(function.ID, 10))
	w.Header().Set(DownloadHeaderFunctionFilename, filepath.Base(function.Path))
	w.Header().Set(DownloadHeaderFunctionPath, function.Path)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)

	if _, err := io.Copy(w, file); err != nil {
		server.InternalServerError(w, r, fmt.Errorf("stream function binary: %w", err))
		return
	}
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

package server

import (
	"errors"
	"fmt"
	"net/http"

	commonapi "github.com/Nesquiko/servermore/pkg/api/common"
)

type ApiError struct {
	commonapi.ErrorDetail
	cause error
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("error %q, status %d, cause %v", e.Title, e.Status, e.cause)
}

type Error struct {
	Cause                error
	Code                 string
	Detail               string
	Status               int
	Title                string
	AdditionalProperties map[string]any
}

func NewApiError(r *http.Request, err Error) *ApiError {
	return &ApiError{
		cause: err.Cause,
		ErrorDetail: commonapi.ErrorDetail{
			Title:                err.Title,
			Code:                 err.Code,
			Detail:               err.Detail,
			Instance:             r.RequestURI,
			Status:               err.Status,
			AdditionalProperties: err.AdditionalProperties,
		},
	}
}

func EncodeError(w http.ResponseWriter, r *http.Request, err Error) {
	apiErr := NewApiError(r, err)
	EncodeApiError(w, r, apiErr)
}

func InternalServerError(w http.ResponseWriter, r *http.Request, error error) {
	EncodeError(w, r, Error{
		Cause:  error,
		Code:   "internal.server.error",
		Title:  "Internal server error",
		Detail: "Unexpected error on server",
		Status: http.StatusInternalServerError,
	})
}

const ContextErrorKey = "error"

func ErrorHandlerFunc(w http.ResponseWriter, r *http.Request, err error) {
	if invalidParamErr, ok := errors.AsType[*commonapi.InvalidParamFormatError](err); ok {
		EncodeError(w, r, fromInvalidParamErr(invalidParamErr))
		return
	} else if requiredParamErr, ok := errors.AsType[*commonapi.RequiredParamError](err); ok {
		EncodeError(w, r, fromRequiredParamErr(requiredParamErr))
		return
	}

	InternalServerError(w, r, err)
}

func EncodeApiError(w http.ResponseWriter, r *http.Request, err *ApiError) {
	SetAPIError(r, err)
	encodeWithContentType(w, err.Status, err.ErrorDetail)
}

const (
	InvalidRequestCode  = "invalid.request"
	InvalidRequestTitle = "Invalid request"

	InvalidParamErrorCode   = "invalid.path.param"
	InvalidParamErrorTitle  = "Invalid path param"
	InvalidParamErrorDetail = "Invalid path param %q"
)

func fromInvalidParamErr(err *commonapi.InvalidParamFormatError) Error {
	return Error{
		Cause:  err,
		Code:   InvalidParamErrorCode,
		Title:  InvalidParamErrorTitle,
		Detail: fmt.Sprintf(InvalidParamErrorDetail, err.ParamName),
		Status: http.StatusBadRequest,
	}
}

const (
	RequiredParamErrorCode   = "required.path.param"
	RequiredParamErrorTitle  = "Required path param"
	RequiredParamErrorDetail = "Required path param %q is missing"
)

func fromRequiredParamErr(err *commonapi.RequiredParamError) Error {
	return Error{
		Cause:  err,
		Code:   RequiredParamErrorCode,
		Title:  RequiredParamErrorTitle,
		Detail: fmt.Sprintf(RequiredParamErrorDetail, err.ParamName),
		Status: http.StatusBadRequest,
	}
}

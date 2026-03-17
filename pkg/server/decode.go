package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const MaxBytes = 1_048_576 // 1MB

func Decode[T any](w http.ResponseWriter, r *http.Request) (T, Error) {
	dst, err := decode[T](w, r)
	if err != nil {
		if decErr, ok := errors.AsType[*decodeErr](err); ok {
			return dst, decodeErrToApiError(decErr)
		} else {
			return dst, errorToApiError(err, DecodingErrorCode)
		}
	}
	return dst, Error{}
}

type decodeErr struct {
	err  error
	code string
}

func (e *decodeErr) Error() string {
	return e.err.Error()
}

const (
	InvalidFieldPrefix = "json: unknown field "
	LargeBodyErrorStr  = "http: request body too large"
)

func decode[T any](w http.ResponseWriter, r *http.Request) (T, error) {
	var dst T
	r.Body = http.MaxBytesReader(w, r.Body, int64(MaxBytes))

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	err := dec.Decode(&dst)
	if err != nil {
		var syntaxErr *json.SyntaxError
		var unmarshalTypeErr *json.UnmarshalTypeError
		var invalidUnmarshalErr *json.InvalidUnmarshalError

		switch {
		case errors.As(err, &syntaxErr):
			return dst, fmt.Errorf(
				"body contains badly-formed JSON (at character %d)",
				syntaxErr.Offset,
			)

		case errors.Is(err, io.ErrUnexpectedEOF):
			return dst, errors.New("body contains badly-formed JSON")

		case errors.As(err, &unmarshalTypeErr):
			if unmarshalTypeErr.Field != "" {
				return dst, fmt.Errorf(
					"body contains incorrect JSON type for field %q",
					unmarshalTypeErr.Field,
				)
			}
			return dst, fmt.Errorf(
				"body contains incorrect JSON type (at character %d)",
				unmarshalTypeErr.Offset,
			)

		case errors.Is(err, io.EOF):
			return dst, errors.New("body must not be empty")

		case strings.HasPrefix(err.Error(), InvalidFieldPrefix):
			fieldName := strings.TrimPrefix(err.Error(), InvalidFieldPrefix)
			return dst, &decodeErr{
				err:  fmt.Errorf("body contains unknown key %s", fieldName),
				code: "invalid.request",
			}

		case err.Error() == LargeBodyErrorStr:
			return dst, fmt.Errorf("body must not be larger than %d bytes", MaxBytes)

		case errors.As(err, &invalidUnmarshalErr):
			return dst, fmt.Errorf("invalid unmarshal target")

		default:
			return dst, err
		}
	}

	err = dec.Decode(&struct{}{})
	if err != io.EOF {
		return dst, errors.New("body must only contain a single JSON value")
	}

	return dst, nil
}

const (
	DecodingErrorCode  = "undecodable.request"
	DecodingErrorTitle = "Request couldn't be decoded"
)

func decodeErrToApiError(err *decodeErr) Error {
	return errorToApiError(err, err.code)
}

func errorToApiError(err error, code string) Error {
	return Error{
		Cause:  err,
		Status: http.StatusBadRequest,
		Title:  DecodingErrorTitle,
		Code:   code,
		Detail: err.Error(),
	}
}

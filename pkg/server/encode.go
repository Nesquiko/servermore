package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

const (
	ContentType     = "Content-Type"
	ApplicationJSON = "application/json"
)

func EncodeResponse[T any](w http.ResponseWriter, r *http.Request, status int, response T) {
	if err := encodeWithContentType(w, status, response); err != nil {
		http.Error(w, "Unexpected encoding error", http.StatusInternalServerError)
	}
}

func encodeWithContentType[T any](
	w http.ResponseWriter,
	code int,
	response T,
) error {
	w.Header().Set(ContentType, ApplicationJSON)
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		return fmt.Errorf(
			"failed to encode response of type %T to content type %s: %w",
			response,
			ApplicationJSON,
			err,
		)
	}
	return nil
}

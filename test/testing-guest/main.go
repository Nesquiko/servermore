package main

import (
	"context"
	"errors"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const (
	PathOK    = "/ok"
	PathNil   = "/nil"
	PathError = "/error"
)

func handler(ctx context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	switch req.GetPath() {
	case PathOK:
		return &guest.InvocationResponse{
			StatusCode: 200,
			Headers:    map[string]string{"content-type": "application/json"},
			Body:       []byte(`{"status":"ok"}`),
		}, nil
	case PathNil:
		return nil, nil
	case PathError:
		return nil, errors.New("testing guest error")
	default:
		return &guest.InvocationResponse{
			StatusCode: 404,
			Headers:    map[string]string{"content-type": "application/json"},
			Body:       []byte(`{"error":"not found"}`),
		}, nil
	}
}

func main() {
	guest.Start(handler)
}

package main

import (
	"context"
	"errors"

	"github.com/Nesquiko/servermore/pkg/guest"
	testingguestconsts "github.com/Nesquiko/servermore/test/testing-guest/consts"
)

func handler(ctx context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	switch req.GetPath() {
	case testingguestconsts.PathOK:
		return &guest.InvocationResponse{
			StatusCode: 200,
			Headers:    map[string]string{"content-type": testingguestconsts.HeaderJSON},
			Body:       []byte(testingguestconsts.BodyOK),
		}, nil
	case testingguestconsts.PathNil:
		return nil, nil
	case testingguestconsts.PathError:
		return nil, errors.New(testingguestconsts.ErrorMessage)
	default:
		return &guest.InvocationResponse{
			StatusCode: 404,
			Headers:    map[string]string{"content-type": testingguestconsts.HeaderJSON},
			Body:       []byte(testingguestconsts.BodyNotFound),
		}, nil
	}
}

func main() {
	guest.Start(handler)
}

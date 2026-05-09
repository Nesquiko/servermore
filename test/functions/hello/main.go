package main

import (
	"context"
	"encoding/json"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const functionName = "hello"

func jsonResponse(status int, payload any) *guest.InvocationResponse {
	body, _ := json.Marshal(payload)
	return &guest.InvocationResponse{
		StatusCode: uint32(status),
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       body,
	}
}

func handler(_ context.Context, _ *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	return jsonResponse(200, map[string]any{
		"function": functionName,
		"message":  "hello from servermore",
	}), nil
}

func main() {
	guest.Start(handler)
}

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const functionName = "counter"

var requestCount atomic.Int64

func jsonResponse(status int, payload any) *guest.InvocationResponse {
	body, _ := json.Marshal(payload)
	return &guest.InvocationResponse{
		StatusCode: uint32(status),
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       body,
	}
}

func handler(_ context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	switch req.GetPath() {
	case "/", "/count":
		count := requestCount.Add(1)
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"count":    count,
		}), nil
	case "/peek":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"count":    requestCount.Load(),
		}), nil
	case "/reset":
		requestCount.Store(0)
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"count":    0,
		}), nil
	case "/slow":
		time.Sleep(50 * time.Millisecond)
		count := requestCount.Add(1)
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"count":    count,
			"slow":     true,
		}), nil
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{
			"function": functionName,
			"error":    "not found",
		}), nil
	}
}

func main() {
	guest.Start(handler)
}

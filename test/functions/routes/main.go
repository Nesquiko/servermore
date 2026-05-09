package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const functionName = "routes"

func jsonResponse(status int, payload any) *guest.InvocationResponse {
	body, _ := json.Marshal(payload)
	return &guest.InvocationResponse{
		StatusCode: uint32(status),
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       body,
	}
}

func parseInt(values url.Values, key string) (int, error) {
	raw := values.Get(key)
	if raw == "" {
		return 0, fmt.Errorf("missing query parameter %q", key)
	}

	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("parse query parameter %q: %w", key, err)
	}

	return value, nil
}

func handler(_ context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	u, err := url.Parse(req.GetPath())
	if err != nil {
		return jsonResponse(http.StatusBadRequest, map[string]any{
			"function": functionName,
			"error":    err.Error(),
		}), nil
	}

	switch u.Path {
	case "/":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"routes": []string{
				"/",
				"/echo",
				"/sleep",
				"/math/add",
				"/math/mul",
				"/status/teapot",
			},
		}), nil
	case "/echo":
		return jsonResponse(http.StatusOK, map[string]any{
			"function":    functionName,
			"method":      req.GetMethod(),
			"path":        req.GetPath(),
			"query":       u.RawQuery,
			"body":        string(req.GetBody()),
			"headers_len": len(req.GetHeaders()),
		}), nil
	case "/sleep":
		time.Sleep(75 * time.Millisecond)
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"slept_ms": 75,
		}), nil
	case "/math/add":
		a, err := parseInt(u.Query(), "a")
		if err != nil {
			return jsonResponse(
				http.StatusBadRequest,
				map[string]any{"function": functionName, "error": err.Error()},
			), nil
		}
		b, err := parseInt(u.Query(), "b")
		if err != nil {
			return jsonResponse(
				http.StatusBadRequest,
				map[string]any{"function": functionName, "error": err.Error()},
			), nil
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"function":  functionName,
			"operation": "add",
			"a":         a,
			"b":         b,
			"result":    a + b,
		}), nil
	case "/math/mul":
		a, err := parseInt(u.Query(), "a")
		if err != nil {
			return jsonResponse(
				http.StatusBadRequest,
				map[string]any{"function": functionName, "error": err.Error()},
			), nil
		}
		b, err := parseInt(u.Query(), "b")
		if err != nil {
			return jsonResponse(
				http.StatusBadRequest,
				map[string]any{"function": functionName, "error": err.Error()},
			), nil
		}
		return jsonResponse(http.StatusOK, map[string]any{
			"function":  functionName,
			"operation": "mul",
			"a":         a,
			"b":         b,
			"result":    a * b,
		}), nil
	case "/status/teapot":
		return jsonResponse(http.StatusTeapot, map[string]any{
			"function": functionName,
			"reason":   "short and stout",
		}), nil
	default:
		return jsonResponse(http.StatusNotFound, map[string]any{
			"function": functionName,
			"error":    "not found",
			"path":     u.Path,
		}), nil
	}
}

func main() {
	guest.Start(handler)
}

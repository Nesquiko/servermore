package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const functionName = "echo"

type headerPair struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func jsonResponse(status int, payload any) *guest.InvocationResponse {
	body, _ := json.Marshal(payload)
	return &guest.InvocationResponse{
		StatusCode: uint32(status),
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       body,
	}
}

func sortedHeaders(headers map[string]string) []headerPair {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	pairs := make([]headerPair, 0, len(headers))
	for _, key := range keys {
		pairs = append(pairs, headerPair{Key: key, Value: headers[key]})
	}

	return pairs
}

func handler(_ context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	switch req.GetPath() {
	case "/", "/inspect":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"method":   strings.ToUpper(req.GetMethod()),
			"path":     req.GetPath(),
			"body":     string(req.GetBody()),
			"headers":  sortedHeaders(req.GetHeaders()),
		}), nil
	case "/headers":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"headers":  sortedHeaders(req.GetHeaders()),
		}), nil
	case "/body":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"body":     string(req.GetBody()),
			"body_len": len(req.GetBody()),
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

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Nesquiko/servermore/pkg/guest"
)

const functionName = "external"

type todo struct {
	UserID    int    `json:"userId"`
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Completed bool   `json:"completed"`
}

func jsonResponse(status int, payload any) *guest.InvocationResponse {
	body, _ := json.Marshal(payload)
	return &guest.InvocationResponse{
		StatusCode: uint32(status),
		Headers:    map[string]string{"content-type": "application/json"},
		Body:       body,
	}
}

func publicAPIURL() string {
	if url := strings.TrimSpace(os.Getenv("PUBLIC_API_URL")); url != "" {
		return url
	}

	return "https://jsonplaceholder.typicode.com/todos/1"
}

func fetchTodo(ctx context.Context) (todo, string, error) {
	apiURL := publicAPIURL()
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return todo{}, apiURL, fmt.Errorf("build upstream request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return todo{}, apiURL, fmt.Errorf("call upstream api: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return todo{}, apiURL, fmt.Errorf(
			"unexpected upstream status %s: %s",
			resp.Status,
			strings.TrimSpace(string(body)),
		)
	}

	var upstream todo
	if err := json.NewDecoder(resp.Body).Decode(&upstream); err != nil {
		return todo{}, apiURL, fmt.Errorf("decode upstream response: %w", err)
	}

	return upstream, apiURL, nil
}

func handler(ctx context.Context, req *guest.InvocationRequest) (*guest.InvocationResponse, error) {
	switch req.GetPath() {
	case "/", "/todo":
		todo, source, err := fetchTodo(ctx)
		if err != nil {
			return nil, err
		}

		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"source":   source,
			"todo":     todo,
		}), nil
	case "/health":
		return jsonResponse(http.StatusOK, map[string]any{
			"function": functionName,
			"source":   publicAPIURL(),
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

package servermoretester

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/Nesquiko/servermore/pkg/server"
)

type deployDoneMsg struct {
	functionIndex int
	name          string
	functionID    string
	err           error
}

func deployFunctionCmd(
	ctx context.Context,
	functionIndex int,
	binaryPath string,
	name string,
) tea.Cmd {
	return func() tea.Msg {
		functionID, err := deployFunction(ctx, binaryPath, name)
		return deployDoneMsg{
			functionIndex: functionIndex,
			name:          name,
			functionID:    functionID,
			err:           err,
		}
	}
}

func deployFunction(ctx context.Context, binaryPath string, name string) (string, error) {
	binaryBytes, err := os.ReadFile(binaryPath)
	if err != nil {
		return "", fmt.Errorf("read compiled binary %q: %w", binaryPath, err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("name", name); err != nil {
		return "", fmt.Errorf("write multipart name: %w", err)
	}

	part, err := writer.CreateFormFile("binary", filepath.Base(binaryPath))
	if err != nil {
		return "", fmt.Errorf("create multipart binary field: %w", err)
	}
	if _, err := part.Write(binaryBytes); err != nil {
		return "", fmt.Errorf("write binary payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, commanderURL+"/functions", &body)
	if err != nil {
		return "", fmt.Errorf("build deploy request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "servermore-tester")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload function: %w", err)
	}
	defer server.Close(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf(
			"deploy failed with %s: %s",
			resp.Status,
			compactText(string(bodyBytes), 180),
		)
	}

	var created struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode deploy response: %w", err)
	}
	if created.ID == 0 {
		return "", fmt.Errorf("commander returned an empty function id")
	}

	return strconv.FormatInt(created.ID, 10), nil
}

func compactText(text string, maxLen int) string {
	cleaned := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if len(cleaned) <= maxLen {
		return cleaned
	}
	if maxLen < 4 {
		return cleaned[:maxLen]
	}
	return cleaned[:maxLen-3] + "..."
}

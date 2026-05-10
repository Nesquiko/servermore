package servermoretester

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	commanderURL   = "http://127.0.0.1:42069"
	gatewayURL     = "http://127.0.0.1:8080"
	heartbeatPath  = "/monitoring/heartbeat"
	composeRelPath = "docker/docker-compose.local-test.yaml"
	tmpFunctions   = "servermore-tester-functions"
)

type compileDoneMsg struct {
	binaries map[string]string
	output   string
	err      error
}

type stackReadyMsg struct {
	output  string
	started bool
	err     error
}

func findProjectRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("read working directory: %w", err)
	}

	for dir := wd; ; dir = filepath.Dir(dir) {
		composeFile := filepath.Join(dir, composeRelPath)
		functionsDir := filepath.Join(dir, "test", "functions")

		composeInfo, composeErr := os.Stat(composeFile)
		functionsInfo, functionsErr := os.Stat(functionsDir)
		if composeErr == nil && functionsErr == nil && !composeInfo.IsDir() &&
			functionsInfo.IsDir() {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}

	return "", fmt.Errorf("could not locate the servermore project root")
}

func compileFunctionsCmd(ctx context.Context, rootDir string, requesters []Requester) tea.Cmd {
	return func() tea.Msg {
		binaries, output, err := compileFunctions(ctx, rootDir, requesters)
		return compileDoneMsg{binaries: binaries, output: output, err: err}
	}
}

func compileFunctions(
	ctx context.Context,
	rootDir string,
	requesters []Requester,
) (map[string]string, string, error) {
	outputDir := filepath.Join(rootDir, "tmp", tmpFunctions)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("create tmp output directory: %w", err)
	}

	binaries := make(map[string]string, len(requesters))
	var logs []string
	for _, requester := range requesters {
		binaryName := requester.BinaryName()
		outputPath := filepath.Join(outputDir, binaryName)
		pkgPath := "./test/functions/" + binaryName
		cmd := exec.CommandContext(ctx, "go", "build", "-o", outputPath, pkgPath)
		cmd.Dir = rootDir
		cmdOutput, err := cmd.CombinedOutput()
		if len(cmdOutput) > 0 {
			logs = append(
				logs,
				fmt.Sprintf("go build %s\n%s", pkgPath, strings.TrimSpace(string(cmdOutput))),
			)
		}
		if err != nil {
			return nil, strings.Join(logs, "\n\n"), fmt.Errorf("compile %s: %w", pkgPath, err)
		}
		binaries[binaryName] = outputPath
	}

	return binaries, strings.Join(logs, "\n\n"), nil
}

func startStackCmd(ctx context.Context, rootDir string) tea.Cmd {
	return func() tea.Msg {
		output, started, err := startStack(ctx, rootDir)
		return stackReadyMsg{output: output, started: started, err: err}
	}
}

func startStack(ctx context.Context, rootDir string) (string, bool, error) {
	composeFile := filepath.Join(rootDir, composeRelPath)
	var logs []string

	_, downOutput, downErr := runCommand(
		ctx,
		rootDir,
		"docker",
		"compose",
		"-f",
		composeFile,
		"down",
		"-v",
		"--remove-orphans",
	)
	if strings.TrimSpace(downOutput) != "" {
		logs = append(logs, downOutput)
	}
	if downErr != nil && ctx.Err() == nil {
		logs = append(logs, downErr.Error())
	}

	_, upOutput, err := runCommand(
		ctx,
		rootDir,
		"docker",
		"compose",
		"-f",
		composeFile,
		"up",
		"-d",
	)
	if strings.TrimSpace(upOutput) != "" {
		logs = append(logs, upOutput)
	}
	if err != nil {
		return strings.Join(logs, "\n\n"), false, err
	}

	waitErr := waitForHTTPReady(ctx, commanderURL+heartbeatPath, 2*time.Minute)
	if waitErr == nil {
		waitErr = waitForHTTPReady(ctx, gatewayURL+heartbeatPath, 2*time.Minute)
	}
	if waitErr != nil {
		return strings.Join(logs, "\n\n"), true, waitErr
	}

	return strings.Join(logs, "\n\n"), true, nil
}

func stopStack(ctx context.Context, rootDir string) error {
	composeFile := filepath.Join(rootDir, composeRelPath)
	_, _, err := runCommand(
		ctx,
		rootDir,
		"docker",
		"compose",
		"-f",
		composeFile,
		"stop",
	)
	return err
}

func runCommand(
	ctx context.Context,
	dir string,
	name string,
	args ...string,
) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	trimmed := strings.TrimSpace(string(output))
	if err != nil {
		return strings.Join(append([]string{name}, args...), " "), trimmed, fmt.Errorf(
			"run %q: %w",
			strings.Join(append([]string{name}, args...), " "),
			err,
		)
	}
	return strings.Join(append([]string{name}, args...), " "), trimmed, nil
}

func waitForHTTPReady(ctx context.Context, target string, timeout time.Duration) error {
	deadlineCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	for {
		req, err := http.NewRequestWithContext(deadlineCtx, http.MethodGet, target, nil)
		if err != nil {
			return fmt.Errorf("build readiness request for %s: %w", target, err)
		}

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest {
				return nil
			}
		}

		select {
		case <-deadlineCtx.Done():
			if err != nil {
				return fmt.Errorf("wait for %s: %w", target, err)
			}
			return fmt.Errorf("wait for %s: last status %s", target, resp.Status)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

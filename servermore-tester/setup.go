package servermoretester

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Nesquiko/servermore/pkg/server"
)

const (
	commanderURL   = "http://127.0.0.1:42069"
	gatewayURL     = "http://127.0.0.1:8080"
	heartbeatPath  = "/monitoring/heartbeat"
	composeRelPath = "docker/docker-compose.local-test.yaml"
	tmpFunctions   = "servermore-tester-functions"
	dozzlePort     = "8080"
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

type stackPollMsg struct {
	output         string
	commanderReady bool
	gatewayReady   bool
	err            error
}

type dozzleStartMsg struct {
	containerID string
	url         string
	err         error
}

type stackShutdownDoneMsg struct {
	err error
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

func pollStackCmd(ctx context.Context, rootDir string) tea.Cmd {
	return func() tea.Msg {
		output, commanderReady, gatewayReady, err := pollStack(ctx, rootDir)
		return stackPollMsg{
			output:         output,
			commanderReady: commanderReady,
			gatewayReady:   gatewayReady,
			err:            err,
		}
	}
}

func startDozzleCmd(ctx context.Context, rootDir string) tea.Cmd {
	return func() tea.Msg {
		containerID, url, err := startDozzle(ctx, rootDir)
		return dozzleStartMsg{containerID: containerID, url: url, err: err}
	}
}

func shutdownStackCmd(rootDir string) tea.Cmd {
	return func() tea.Msg {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return stackShutdownDoneMsg{err: stopStack(cleanupCtx, rootDir)}
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

	return strings.Join(logs, "\n\n"), true, nil
}

func pollStack(ctx context.Context, rootDir string) (string, bool, bool, error) {
	composeFile := filepath.Join(rootDir, composeRelPath)
	_, output, err := runCommand(
		ctx,
		rootDir,
		"docker",
		"compose",
		"-f",
		composeFile,
		"logs",
		"--no-color",
		"--tail",
		"10",
	)
	if err != nil {
		return output, false, false, err
	}

	commanderReady := checkHTTPReady(ctx, commanderURL+heartbeatPath)
	gatewayReady := checkHTTPReady(ctx, gatewayURL+heartbeatPath)
	return output, commanderReady, gatewayReady, nil
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
		"down",
	)
	return err
}

func startDozzle(ctx context.Context, rootDir string) (string, string, error) {
	name := fmt.Sprintf("servermore-dozzle-%d", time.Now().UnixNano())
	_, output, err := runCommand(
		ctx,
		rootDir,
		"docker",
		"run",
		"-d",
		"--name",
		name,
		"-v",
		"/var/run/docker.sock:/var/run/docker.sock",
		"-v",
		"dozzle_data:/data",
		"-p",
		"0:8080",
		"amir20/dozzle:latest",
	)
	if err != nil {
		return "", "", err
	}

	containerID := strings.TrimSpace(output)
	if containerID == "" {
		return "", "", fmt.Errorf("docker run did not return a container id")
	}

	_, portOutput, err := runCommand(ctx, rootDir, "docker", "port", containerID, dozzlePort)
	if err != nil {
		return containerID, "", err
	}

	hostPort, err := parsePublishedPort(portOutput)
	if err != nil {
		return containerID, "", err
	}

	return containerID, fmt.Sprintf("http://127.0.0.1:%s", hostPort), nil
}

func parsePublishedPort(output string) (string, error) {
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		matches := portSuffixPattern.FindStringSubmatch(line)
		if len(matches) == 2 {
			return matches[1], nil
		}
	}
	return "", fmt.Errorf("could not parse published port from %q", output)
}

var portSuffixPattern = regexp.MustCompile(`:(\d+)$`)

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

func checkHTTPReady(ctx context.Context, target string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, target, nil)
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer server.Close(resp.Body)

	return resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusBadRequest
}

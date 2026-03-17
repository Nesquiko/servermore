package e2e

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// subdirInTempDir creates new directory in the /tmp/servermore-*/subdir,
// However the subdir is not created, to let the app handle the creation itself
func subdirInTempDir(subdir string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "servermove-*")
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, subdir), nil
}

func randomFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for random port: %w", err)
	}
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected listener addr type %T", listener.Addr())
	}

	return strconv.Itoa(tcpAddr.Port), nil
}

// waitForReady calls the specified endpoint until it gets a 200
// response or until the context is cancelled or the timeout is reached.
func waitForReady(
	ctx context.Context,
	timeout time.Duration,
	interval time.Duration,
	endpoint string,
) error {
	client := http.Client{}
	startTime := time.Now()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			nestedErr := errors.Unwrap(errors.Unwrap(err))
			if nestedErr.Error() == "connect: connection refused" {
				time.Sleep(interval)
				continue
			}
			return fmt.Errorf("error while waiting for ready: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			slog.Info("server is ready after", "time", time.Since(startTime))
			resp.Body.Close()
			return nil
		}
		resp.Body.Close()

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if time.Since(startTime) >= timeout {
				return fmt.Errorf("timeout reached while waiting for endpoint")
			}
			time.Sleep(interval)
		}
	}
}

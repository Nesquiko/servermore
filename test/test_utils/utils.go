package testutils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/server"
	"google.golang.org/grpc/connectivity"
)

var (
	BuildDir          string
	TestingBinaryPath string
)

func init() {
	_, file, _, ok := runtime.Caller(0)
	assert.That(ok, "failed to resolve test utils path")

	testUtilsDir := filepath.Dir(file)
	repoRoot := filepath.Clean(filepath.Join(testUtilsDir, "..", ".."))
	BuildDir = filepath.Join(repoRoot, "tmp")
	TestingBinaryPath = filepath.Join(BuildDir, "testing-guest")
}

// SubdirInTempDir creates new directory in the /tmp/servermore-*/subdir,
// However the subdir is not created, to let the app handle the creation itself
func SubdirInTempDir(subdir string) (string, error) {
	tmpDir, err := os.MkdirTemp(BuildDir, "servermove-*")
	if err != nil {
		return "", err
	}
	return filepath.Join(tmpDir, subdir), nil
}

func RandomFreePort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for random port: %w", err)
	}
	defer server.Close(listener)

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected listener addr type %T", listener.Addr())
	}

	return strconv.Itoa(tcpAddr.Port), nil
}

func DeleteIfExists(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// WaitForHttpReady calls the specified endpoint until it gets a 200
// response or until the context is cancelled or the timeout is reached.
func WaitForHttpReady(ctx context.Context, serverLabel string, fullEndpointPath string) error {
	return WaitForHttpReadyWithTiming(
		ctx,
		serverLabel,
		1*time.Second,
		100*time.Millisecond,
		fullEndpointPath,
	)
}

func WaitForHttpReadyWithTiming(
	ctx context.Context,
	serverLabel string,
	timeout time.Duration,
	interval time.Duration,
	fullEndpointPath string,
) error {
	client := http.Client{}
	startTime := time.Now()
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullEndpointPath, nil)
		if err != nil {
			return fmt.Errorf("failed to create http request: %w", err)
		}

		resp, err := client.Do(req)
		if err != nil {
			nestedErr := errors.Unwrap(errors.Unwrap(err))
			if nestedErr.Error() == "connect: connection refused" {
				time.Sleep(interval)
				continue
			}
			return fmt.Errorf("error while waiting for http ready: %w", err)
		}

		if resp.StatusCode == http.StatusOK {
			slog.Info(
				"http server is ready after",
				"server.label", serverLabel,
				"time", time.Since(startTime),
			)
			server.Close(resp.Body)
			return nil
		}
		defer server.Close(resp.Body)

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if time.Since(startTime) >= timeout {
				return fmt.Errorf(
					"timeout reached while waiting for http endpoint %q",
					fullEndpointPath,
				)
			}
			time.Sleep(interval)
		}
	}
}

// WaitForGrpcReady calls the heartbeat function until it succeds
// or until the context is cancelled or the timeout is reached.
func WaitForGrpcReady(ctx context.Context, serverLabel string, addr string) error {
	return WaitForGrpcReadyWithTiming(ctx, serverLabel, 3*time.Second, 100*time.Millisecond, addr)
}

func WaitForGrpcReadyWithTiming(
	ctx context.Context,
	serverLabel string,
	timeout time.Duration,
	interval time.Duration,
	addr string,
) error {
	waitingOpts := server.MonitoringOpts{
		Env:     server.TEST,
		AppName: "grpc-ready-check",
	}
	startTime := time.Now()
	conn, err := server.LoggingGrpcClient(addr, waitingOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize grpc connection: %w", err)
	}
	defer server.Close(conn)
	conn.Connect()

	for {
		state := conn.GetState()

		if state == connectivity.Ready {
			slog.Info(
				"grpc server is ready after",
				"server.label", serverLabel,
				"time", time.Since(startTime),
			)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if time.Since(startTime) >= timeout {
				return fmt.Errorf("timeout reached while waiting for grpc server to be ready")
			}
			time.Sleep(interval)
		}
	}
}

func AddRandomPart(s string) string {
	bytes := make([]byte, 4)
	_, err := rand.Read(bytes)
	assert.NoError(err)
	return fmt.Sprintf("%s-%s", s, hex.EncodeToString(bytes))
}

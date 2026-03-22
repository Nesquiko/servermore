package testutils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/Nesquiko/servermore/pkg/runner"
	"github.com/Nesquiko/servermore/pkg/server"
	testqueries "github.com/Nesquiko/servermore/test/test_utils/queries.gen"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/connectivity"
)

var TestingBinaryPath = filepath.Join("..", "..", "tmp", "testing-guest")

// SubdirInTempDir creates new directory in the /tmp/servermore-*/subdir,
// However the subdir is not created, to let the app handle the creation itself
func SubdirInTempDir(subdir string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "servermove-*")
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
	defer listener.Close()

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return "", fmt.Errorf("unexpected listener addr type %T", listener.Addr())
	}

	return strconv.Itoa(tcpAddr.Port), nil
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
			resp.Body.Close()
			return nil
		}
		resp.Body.Close()

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
	return WaitForGrpcReadyWithTiming(ctx, serverLabel, 1*time.Second, 100*time.Millisecond, addr)
}

func WaitForGrpcReadyWithTiming(
	ctx context.Context,
	serverLabel string,
	timeout time.Duration,
	interval time.Duration,
	addr string,
) error {
	waitingOpts := server.MonitoringOpts{
		IsDev:      true,
		AppName:    "grpc-ready-check",
		AppVersion: "n/a",
		Env:        "TEST",
	}
	startTime := time.Now()
	conn, err := server.InstrumentedGrpcClient(addr, waitingOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize grpc connection: %w", err)
	}
	defer runner.CloseConn(conn)
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

func TestDB(t *testing.T, dbPath string) *testqueries.Queries {
	db, err := sql.Open("sqlite", dbPath)
	require.NoError(t, err)
	require.NoError(t, db.Ping())

	return testqueries.New(db)
}

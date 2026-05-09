package runner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/guest"
	"github.com/Nesquiko/servermore/pkg/server"
	grpc "google.golang.org/grpc"
)

type NetworkAddr = string

const NativeRuntimeType = "native"

type FunctionRuntime interface {
	Type() string
	Start(context.Context, server.AbsolutePath) error
	Invoke(context.Context, *guest.InvocationRequest) (*guest.InvocationResponse, error)
	Stop()
}

// For now only the native runtime is implemented
func DetermineFuncRuntime() FunctionRuntime {
	return &nativeRuntime{}
}

type nativeRuntime struct {
	cmd    *exec.Cmd
	addr   NetworkAddr
	client guest.GuestClient
	conn   *grpc.ClientConn
}

var _ FunctionRuntime = (*nativeRuntime)(nil)

// Type implements [FunctionRuntime].
func (n *nativeRuntime) Type() string {
	return NativeRuntimeType
}

// Start implements [FunctionRuntime].
func (n *nativeRuntime) Start(
	ctx context.Context,
	funcPath server.AbsolutePath,
) error {
	startTime := time.Now()
	meta := server.GetInstanceStartMeta(ctx)
	assert.That(meta != nil, "meta was nil")
	defer func() {
		meta.StartTook = time.Since(startTime)
	}()

	port, err := FreePort()
	if err != nil {
		return fmt.Errorf("failed to acquire random free port: %w", err)
	}

	instanceAddr := fmt.Sprintf("127.0.0.1:%d", port)
	meta.InstanceAddr = instanceAddr

	instanceCmd, startRetries, err := startWithRetry(funcPath, instanceAddr)
	meta.StartRetries = startRetries
	if err != nil {
		return fmt.Errorf("failed to start the binary: %w", err)
	}

	conn, err := server.GrpcClient(instanceAddr)
	if err != nil {
		return fmt.Errorf("failed to initialize instance at addr=%q client: %w", instanceAddr, err)
	}
	instanceClient := guest.NewGuestClient(conn)

	readyCh := make(chan heartbeatReadyResult, 1)
	go func() {
		took, retries, err := waitForHeartbeat(ctx, instanceClient)
		readyCh <- heartbeatReadyResult{took: took, retries: retries, err: err}
	}()

	readyResult := <-readyCh
	meta.HeartbeatTook = readyResult.took
	meta.HeartbeatRetries = readyResult.retries
	if readyResult.err != nil {
		server.Close(conn)
		closeCmd(instanceCmd)
		return fmt.Errorf(
			"instance binary %q at addr %q Heartbeat failed: %w",
			funcPath,
			instanceAddr,
			readyResult.err,
		)
	}

	n.cmd = instanceCmd
	n.addr = instanceAddr
	n.client = instanceClient
	n.conn = conn
	return nil
}

// Invoke implements [FunctionRuntime].
func (n *nativeRuntime) Invoke(
	ctx context.Context,
	req *guest.InvocationRequest,
) (*guest.InvocationResponse, error) {
	return n.client.InvokeFunction(ctx, req)
}

// Stop implements [FunctionRuntime].
func (n *nativeRuntime) Stop() {
	server.Close(n.conn, slog.String("addr", n.addr))
	closeCmd(n.cmd)
}

// FreePort asks the kernel for a free open port that is ready to use.
func FreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer server.Close(l)
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

const ExitCmdWaitDelay = 5 * time.Second

func closeCmd(cmd *exec.Cmd) {
	assert.That(cmd != nil, "caller send nil cmd")

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		slog.Error("sending signal to cmd failed", "error", err, "cmd", cmd.String())
	}
	cmd.WaitDelay = ExitCmdWaitDelay
	if err := cmd.Wait(); err != nil {
		slog.Error("waiting for cmd to shutdown errored", "error", err, "cmd", cmd.String())
	}
}

const (
	HeartbeatTimeout = 5 * time.Second
)

func waitForHeartbeat(ctx context.Context, client guest.GuestClient) (time.Duration, int, error) {
	startTime := time.Now()
	timer := time.NewTimer(HeartbeatTimeout)
	defer timer.Stop()

	attempts := 0
	errs := make([]error, 0)
	for {
		attempts++

		_, err := client.Heartbeat(ctx, &guest.HeartbeatRequest{})
		if nil == err {
			return time.Since(startTime), attempts - 1, nil
		}
		errs = append(errs, err)

		select {
		case <-ctx.Done():
			return time.Since(startTime), attempts - 1, ctx.Err()
		case <-timer.C:
			return time.Since(
					startTime,
				), attempts - 1, fmt.Errorf(
					"heartbeat didn't succed, errors: %+v",
					errs,
				)
		case <-time.After(time.Duration(attempts) * 10 * time.Millisecond):
		}
	}
}

const MaxStartRetries = 10

type heartbeatReadyResult struct {
	took    time.Duration
	retries int
	err     error
}

func startWithRetry(funcPath server.AbsolutePath, instanceAddr string) (*exec.Cmd, int, error) {
	attempts := 0
	for {
		attempts++

		instanceCmd := exec.Command(funcPath)
		instanceCmd.Env = append(
			instanceCmd.Env,
			fmt.Sprintf("%s=%s", guest.GuestAddrEnvVar, instanceAddr),
		)

		err := instanceCmd.Start()
		if nil == err {
			return instanceCmd, attempts - 1, nil
		} else if !errors.Is(err, syscall.ETXTBSY) {
			return nil, attempts - 1, fmt.Errorf("retry failed with not ETXTBSY error: %w", err)
		}

		if attempts >= MaxStartRetries {
			return nil, attempts - 1, fmt.Errorf("starting cmd failed multiple times: %w", err)
		}
		time.Sleep(time.Duration(attempts) * 10 * time.Millisecond)
	}
}

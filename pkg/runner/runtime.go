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
	assert.That(meta != nil, "instance start meta was nil")

	port, err := FreePort()
	if err != nil {
		return fmt.Errorf("failed to acquire random free port: %w", err)
	}

	instanceAddr := fmt.Sprintf("127.0.0.1:%d", port)
	meta.InstanceAddr = instanceAddr

	instanceCmd, err := startWithRetry(funcPath, instanceAddr)
	if err != nil {
		return fmt.Errorf("failed to start the binary: %w", err)
	}

	conn, err := server.GrpcClient(instanceAddr)
	if err != nil {
		return fmt.Errorf("failed to initialize instance at addr=%q client: %w", instanceAddr, err)
	}
	instanceClient := guest.NewGuestClient(conn)

	readyCh := make(chan error, 1)
	go func() {
		readyCh <- waitForHeartbeat(ctx, instanceClient)
	}()

	readyErr := <-readyCh
	if readyErr != nil {
		CloseConn(conn)
		closeCmd(instanceCmd)
		meta.StartTook = time.Since(startTime)
		return fmt.Errorf(
			"instance binary %q at addr %q Heartbeat failed: %w",
			funcPath,
			instanceAddr,
			readyErr,
		)
	}

	n.cmd = instanceCmd
	n.addr = instanceAddr
	n.client = instanceClient
	n.conn = conn
	meta.StartTook = time.Since(startTime)
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
	CloseConn(n.conn, slog.String("addr", n.addr))
	closeCmd(n.cmd)
}

// FreePort asks the kernel for a free open port that is ready to use.
func FreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func CloseConn(conn *grpc.ClientConn, logAttrs ...any) {
	assert.That(conn != nil, "caller send nil connection")
	if err := conn.Close(); err != nil {
		logAttrs = append(logAttrs, slog.Any("error", err))
		slog.Error("closing client connection failed", logAttrs...)
	}
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

func waitForHeartbeat(ctx context.Context, client guest.GuestClient) error {
	timer := time.NewTimer(HeartbeatTimeout)
	defer timer.Stop()

	retries := 0
	errs := make([]error, 0)
	for {
		retries++

		_, err := client.Heartbeat(ctx, &guest.HeartbeatRequest{})
		if nil == err {
			return nil
		}
		errs = append(errs, err)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return fmt.Errorf("heartbeat didn't succed, errors: %+v", errs)
		case <-time.After(time.Duration(retries) * 10 * time.Millisecond):
		}
	}
}

const MaxStartRetries = 10

func startWithRetry(funcPath server.AbsolutePath, instanceAddr string) (*exec.Cmd, error) {
	retries := 0
	for {
		retries++

		instanceCmd := exec.Command(funcPath)
		instanceCmd.Env = append(
			instanceCmd.Env,
			fmt.Sprintf("%s=%s", guest.GuestAddrEnvVar, instanceAddr),
		)

		err := instanceCmd.Start()
		if nil == err {
			return instanceCmd, nil
		} else if !errors.Is(err, syscall.ETXTBSY) {
			return nil, fmt.Errorf("retry failed with not ETXTBSY error: %w", err)
		}

		if retries >= MaxStartRetries {
			return nil, fmt.Errorf("starting cmd failed multiple times: %w", err)
		}
		time.Sleep(time.Duration(retries) * 10 * time.Millisecond)
	}
}

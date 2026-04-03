# Working version

The working version of our serverless platform as of 3.4.2026 and git tag `v0.1.1`.
This version is simple and focuses mainly on getting the request flow implemented.
Due to this there are certain features that such a platform should have but does not:

- **User separation:** Now each user can call each function, there is no concept of a user.
  If a user tries to upload already existing function binary (matching hashes of the binary),
  they will get an error back.
- **Runtime:** Only a native runtime (running the binaries directly on the runner) is implemented.
  Since functions must be gRPC servers, their interface is at network level. Thanks
  to this new runtimes can be added and will work without modifying anything else
  if they adhere to the gRPC network interface.

## What you can run

You can download [mise](https://mise.jdx.dev/getting-started.html), which is tool management,
and task runner (something like Make but for 21st century).

After downloading it, run `mise install` to download necessary tools, and then
run `mise test`. This command will run all the tests in the project, which includes:

- small unit tests,
- `Commander` integration tests which test its HTTP API for uploading new function binaries (`test/commander/create_function_test.go`),
- `Runner` integration tests which test ist gRPC API for heartbeats (`test/runner/heartbeat_test.go`),
  preparing function instances (`test/runner/prepare_test.go`) and invoking function instances (`test/runner/invoke_test.go`).

## Implemented

The whole request flow, as documented in project's README.md, is not yet implemented.
But work on each of the 4 components was done:

### Gateway

Gateway has a public HTTP API for incoming user requests. It parses the `function_id`
from the request path. This minimal implementation can be found in `cmd/gateway/main.go`.

#### Remaining work:

1. Calling `Commander` for where to send request.
2. Sending invocation request to runner.
3. Responding back to the user's request.

### Commander

Commander has a HTTP API for uploading function binaries (used by users) and for
downloading binaries (mainly used by runners). Code for these HTTP handlers is in
`pkg/commander/handlers.go`. `Commander` persists data into a SQLite database.
We choose SQLite because it is simple and its setup is really quick. The persistence
level code is in `pkg/commander/db.go`, and the SQLite implementation is behind
an interface, which means, if we want something more robust we can swap the implementation
for a PostgreSQL one if needed.

#### Remaining work:

1. `Runner` registration.
2. `Runner` heartbeat monitoring.
3. Routing cache used to determine to which runner to route.
4. Routing logic.

### Runner

Most of the work went into making `Runner` component implemented. It has
complete gRPC server (in `pkg/runner/grpc.go`) through which:

- heartbeat status of the runner can be pulled,
- function instances can be prepared,
- and function instances can be invoked.

It starts/stops instances, and keeps request queues for each. The nature of runner
is very parallel, thus access to the running instances states heavily uses
mutexes and atomics (code in `pkg/runner/instances.go`).

For now there is only one type of function runtime, native runtime (code in `pkg/runner/runtime.go`).
This runtime spawns function binaries as child processes. This is not secure
as each user defined function has access to the whole runner system without
any separation between the functions. This was a tradeoff for implementation speed.
Later we can implemented new, more secure runtimes, e.g. running a binary in docker,
or even different types of runtimes, e.g. Javascript, Java, C+, Python.

### Guest

Guests are user supplied binaries that are the main point of scaling in our serverless platform.
For simplicity, they must be a relatively long lived gRPC servers (`pkg/guest/guest.proto`).
This puts their interface at the network level, thanks to which we can support
any language which can implement a gRPC server.

We have only a Go SDK for our platform, which is defined in `pkg/guest/entrypoint.go`,
and is used in `test/testing-guest/main.go`. The SDK is inspired by AWS Lambda Go SDK.

## To be implemented

1. Complete `Gateway`.
2. Complete `Commander`.
3. Create a E2E test which tests all the components together.

## Nice to haves if enough time

- `Commander` web UI served by the commander, which enables uploading function
  binaries and monitoring the system.
- Grafana+Loki+Mimir+Tempo monitoring stack: the goal would be to run the E2E test and
  capture all monitoring data and visualize it in the Grafana stack.
- More runtime type diversity.

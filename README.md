# Servermore

Servermore is a simplified serverless platform written in Go.

## DEV setup

This project uses [mise](https://mise.jdx.dev/) for tool management. Install
it and run `mise install`. You have everything you need for the project (hopefully).

## Overview

The system is split into four components:

- `Gateway`: public HTTP entrypoint
- `Commander`: control plane and scheduler
- `Runner`: execution node hosting function instances
- `Guest`: user function runtime exposed through an SDK and invoked over gRPC

## Tech Stack

- Go
- SQLite behind an interface with `sqlc`
- Valkey
- gRPC for internal communication
- HTTP for public ingress through `gateway`
- env vars and YAML config
- `slog` for logging

## Request Flow

1. A client sends an HTTP request to `Gateway`.
2. The first path segment is the `function_id`.
3. `Gateway` strips that `function_id` from the forwarded request path.
4. `Gateway` asks `Commander` where the request should go.
5. `Commander` selects a `Runner` and a concrete `instance_id`.
6. `Gateway` sends the invocation to that `Runner`.
7. `Runner` forwards the normalized invocation to the selected `Guest` over gRPC.
8. The response goes back through `Runner` and `Gateway` to the client.

## Responsibilities

### Gateway

- exposes the public HTTP API
- extracts `function_id` from the path
- normalizes incoming HTTP requests
- propagates tracing context
- asks `Commander` to route each request
- invokes the selected `Runner`
- retries once when routing is stale

### Commander

- stores functions, runners, and routing state
- receives runner registration and pings
- schedules new instances
- resolves requests to a concrete `runner` + `instance_id`
- tracks load and queue depth
- avoids routing to unavailable runners

### Runner

- exposes a gRPC API to `Commander`
- prepares function instances on demand
- starts and stops guest processes
- waits for guest readiness
- invokes guests over gRPC
- reports queue depth and liveness to `Commander`

### Guest

- is the user function process
- runs as a long-lived gRPC server
- receives normalized invocation requests
- returns normalized invocation responses
- is started through the guest SDK

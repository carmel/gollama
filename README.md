<div align="right">
  <span>[<a href="./README.md">English</a>]<span>
  </span>[<a href="./README_CN.md">简体中文</a>]</span>
</div>

# gollama

A lightweight gateway for starting `llama-server` locally and providing a compatible OpenAI Chat Completions interface. It automatically launches the model service, waits for it to be ready, supports concurrent queuing, and forwards `/v1/chat/completions` requests to the upstream.

## Features

- Start and daemonize `llama-server` (automatic restart)
- Readiness monitoring and health check endpoint
- Concurrent queue control (return 429 on timeout)
- Forwarding compatible with OpenAI `/v1/chat/completions` (including SSE streaming)

## Prerequisites

1. Prepare the `llama-server` executable (e.g., from llama.cpp).
2. Prepare your GGUF model file.
3. Ensure your Go version matches the one specified in `go.mod`.

## Quick Start

1. Copy and adjust the configuration (recommended to start from `cmd/config.yaml`):

```yaml
server:
  addr: ":8081"
engine:
  bin: "./bin/llama-server"
  args:
    - "-m"
    - "/absolute/path/to/model.gguf"
    - "--host"
    - "127.0.0.1"
    - "--port"
    - "8080"
  base_url: "http://127.0.0.1:8080"
  ready_pattern: "listening|ready|server is running"
  start_timeout: "30s"
  stop_timeout: "10s"
  restart_backoff: "2s"
queue:
  max_concurrency: 1
  wait_timeout: "60s"
logger:
  log-path: "./server.log"
  log-level: debug
  max-size: 2
  max-age: 60
```

Note: The relative path for `engine.bin` is resolved relative to the current working directory of the starting process. If you run from the repository root, it is recommended to use `./bin/llama-server` or an absolute path.

2. Start the service (from the repository root):

```bash
go run ./cmd -config cmd/config.yaml
```

3. Check health:

```bash
curl http://127.0.0.1:8081/healthz
```

A response of `{"status":"ok"}` indicates the upstream is ready.

4. Send a chat request:

```bash
curl http://127.0.0.1:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "local",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Hello, introduce yourself"}
    ],
    "stream": false
  }'
```

For streaming output, set `"stream": true` and ensure your client supports SSE.

## Streaming Output Usage

Streaming output is based on SSE (`text/event-stream`). Clients must disable buffering and continuously read the response stream.

1. View streaming output with `curl`:

```bash
curl -N http://127.0.0.1:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "local",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "Summarize this paragraph in three sentences"}
    ],
    "stream": true
  }'
```

Notes:

- `-N` disables curl's output buffering to avoid waiting until the stream ends.
- The response will be pushed as multiple `data:` events until completion.

2. Simple Go example to read the stream:

```go
package main

import (
	"bufio"
	"bytes"
	"net/http"
)

func main() {
	body := []byte(`{
  "model": "local",
  "messages": [{"role":"user","content":"Give me a short example"}],
  "stream": true
}`)

	req, _ := http.NewRequest("POST", "http://127.0.0.1:8081/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		// Each line may be "data: {...}" or an empty line
		_ = line
	}
}
```

## Configuration Options

- `server.addr`: Gateway listening address.
- `engine.bin`: Path to the `llama-server` executable.
- `engine.args`: Array of arguments used to start `llama-server`.
- `engine.base_url`: Upstream service address (gateway forwarding target).
- `engine.ready_pattern`: Regex pattern to match readiness in stdout/stderr logs.
- `engine.start_timeout`: Timeout for startup wait.
- `engine.stop_timeout`: Timeout for shutdown wait.
- `engine.restart_backoff`: Restart interval after upstream exit.
- `queue.max_concurrency`: Maximum number of concurrent requests.
- `queue.wait_timeout`: Queue wait timeout (returns 429 on timeout).
- `logger`: Logging configuration (depends on `github.com/carmel/go-pkg/logger`).

## API

- `POST /v1/chat/completions`
  - Compatible with OpenAI Chat Completions request body
  - When `stream=true`, forwards via SSE
- `GET /healthz`
  - Returns 200 + `{"status":"ok"}` when upstream is ready
  - Returns 503 + `{"status":"engine_not_ready"}` when not ready

## FAQ

- **Returns 429 (queue timeout)**: Concurrency exceeded `queue.max_concurrency` and wait time exceeded `queue.wait_timeout`.
- **Service fails to start**: Check that `engine.bin` path and `engine.args` are correct; review logs to confirm `ready_pattern` matches.
- **Streaming not returning**: Client must support `text/event-stream` and must not buffer the output.

## Local Binary Build

```bash
go build -ldflags="-w -s" -trimpath -o gollama ./cmd
./gollama -config cmd/config.yaml
```

## Benchmarking

- `make test-benchmark`

```log
go test -run TestBenchmarkSingleService -v -timeout 10m ./cmd
=== RUN   TestBenchmarkSingleService
    benchmark_test.go:51: Starting benchmark for http://192.168.3.21:8090
    benchmark_test.go:52: Config: concurrency=4, requests=20, max_tokens=128
    benchmark_test.go:268:
        ========== Benchmark Results ==========
    benchmark_test.go:269: Service URL:         http://192.168.3.21:8090
    benchmark_test.go:270: Total Requests:      20
    benchmark_test.go:271: Successful:          20
    benchmark_test.go:272: Failed:              0
    benchmark_test.go:273: Duration:            71.41s
    benchmark_test.go:274: Success Rate:        100.0%
    benchmark_test.go:281:
        --- Latency (ms) ---
    benchmark_test.go:282: Min:                 4179.05
    benchmark_test.go:283: Avg:                 13238.83
    benchmark_test.go:284: P50:                 14157.76
    benchmark_test.go:285: P95:                 14752.90
    benchmark_test.go:286: P99:                 14752.90
    benchmark_test.go:287: Max:                 14752.90
    benchmark_test.go:289:
        --- Throughput ---
    benchmark_test.go:290: Requests/sec:        0.28
    benchmark_test.go:291: Total Tokens:        1280
    benchmark_test.go:292: Avg Tokens/Request:  64
    benchmark_test.go:293: Avg Tokens/sec:      4.83
    benchmark_test.go:295:
        ========================================
--- PASS: TestBenchmarkSingleService (71.41s)
PASS
ok  	gollama/cmd	72.464s
```

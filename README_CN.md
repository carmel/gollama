<div align="right">
  <span>[<a href="./README.md">English</a>]<span>
  </span>[<a href="./README_CN.md">简体中文</a>]</span>
</div>

# gollama

一个用于在本地启动 `llama-server` 并提供兼容 OpenAI Chat Completions 接口的轻量级网关。它会自动拉起模型服务、等待就绪、支持并发排队，并将 `/v1/chat/completions` 请求转发到上游。

## 功能

- 启动与守护 `llama-server`（自动重启）
- readiness 监测与健康检查接口
- 并发队列控制（超时返回 429）
- 兼容 OpenAI `/v1/chat/completions` 的转发（含 SSE 流式）

## 运行前准备

1. 准备好 `llama-server` 可执行文件（例如 llama.cpp 的 server）。
2. 准备好 GGUF 模型文件。
3. 确保 `go` 版本与 `go.mod` 匹配。

## 快速开始

1. 复制并调整配置（建议从 `cmd/config.yaml` 开始）：

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

注意：`engine.bin` 的相对路径是相对于启动进程的当前工作目录解析的。如果你从仓库根目录运行，建议使用 `./bin/llama-server` 或绝对路径。

2. 启动服务（从仓库根目录）：

```bash
go run ./cmd -config cmd/config.yaml
```

3. 访问健康检查：

```bash
curl http://127.0.0.1:8081/healthz
```

返回 `{"status":"ok"}` 表示上游已就绪。

4. 发送聊天请求：

```bash
curl http://127.0.0.1:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "local",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "你好，介绍一下你自己"}
    ],
    "stream": false
  }'
```

如果需要流式输出，把 `"stream": true`，并确保客户端支持 SSE。

## 流式输出用法

流式输出基于 SSE（`text/event-stream`）。客户端需要关闭缓冲并持续读取响应流。

1. 使用 `curl` 查看流式输出：

```bash
curl -N http://127.0.0.1:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "local",
    "messages": [
      {"role": "system", "content": "You are a helpful assistant."},
      {"role": "user", "content": "用三句话概括这段话"}
    ],
    "stream": true
  }'
```

说明：

- `-N` 用于禁用 `curl` 的输出缓冲，避免等待流结束才显示。
- 响应会以多段 `data:` 事件推送，直到结束。

2. 使用 Go 简单读取流：

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
  "messages": [{"role":"user","content":"给我一个简短的示例"}],
  "stream": true
}`)

	req, _ := http.NewRequest("POST", "http://127.0.0.1:8081/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	resp, _ := http.DefaultClient.Do(req)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		// 每行可能是 "data: {...}" 或空行
		_ = line
	}
}
```

## 配置说明

- `server.addr`: 网关监听地址。
- `engine.bin`: `llama-server` 的可执行文件路径。
- `engine.args`: 启动 `llama-server` 的参数数组。
- `engine.base_url`: 上游服务地址（网关转发目标）。
- `engine.ready_pattern`: 从 stdout/stderr 中匹配就绪日志的正则。
- `engine.start_timeout`: 启动等待超时。
- `engine.stop_timeout`: 停止等待超时。
- `engine.restart_backoff`: 上游退出后的重启间隔。
- `queue.max_concurrency`: 同时处理的请求数。
- `queue.wait_timeout`: 排队等待超时，超时返回 429。
- `logger`: 日志配置（依赖 `github.com/carmel/go-pkg/logger`）。

## API

- `POST /v1/chat/completions`
  - 兼容 OpenAI Chat Completions 请求体
  - `stream=true` 时使用 SSE 透传
- `GET /healthz`
  - 上游就绪返回 200 + `{"status":"ok"}`
  - 未就绪返回 503 + `{"status":"engine_not_ready"}`

## 常见问题

- **返回 429（queue timeout）**：并发超过 `queue.max_concurrency` 且等待超过 `queue.wait_timeout`。
- **服务启动失败**：检查 `engine.bin` 路径与 `engine.args` 是否正确；查看日志确认是否匹配 `ready_pattern`。
- **流式无法返回**：客户端需支持 `text/event-stream`，并不要缓冲输出。

## 本地构建（二进制）

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

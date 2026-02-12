build:
	go build -ldflags="-w -s" -trimpath -o gollama ./cmd

# Benchmark tests - run one service at a time
test-benchmark:
	go test -run TestBenchmarkSingleService -v -timeout 10m ./cmd

test-benchmark-high-load:
	go test -run TestBenchmarkHighLoad -v -timeout 20m ./cmd

test-benchmark-long-text:
	go test -run TestBenchmarkLongText -v -timeout 20m ./cmd

test-benchmark-low-latency:
	go test -run TestBenchmarkLowLatency -v -timeout 10m ./cmd

test-benchmark-stress:
	go test -run TestBenchmarkStress -v -timeout 30m ./cmd

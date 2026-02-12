package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"
)

const serviceURL = "http://192.168.3.21:8090"

type BenchmarkMetrics struct {
	Latencies     []time.Duration
	TokenCounts   []int
	Errors        []string
	SuccessCount  int
	FailureCount  int
	TotalDuration time.Duration
}

type BenchmarkResult struct {
	TotalRequests      int
	SuccessfulRequests int
	FailedRequests     int
	AvgLatency         time.Duration
	MinLatency         time.Duration
	MaxLatency         time.Duration
	P50Latency         time.Duration
	P95Latency         time.Duration
	P99Latency         time.Duration
	AvgTokensPerSec    float64
	TotalTokens        int
	ThroughputReqSec   float64
	Duration           time.Duration
}

// TestBenchmarkSingleService 测试单个服务的性能
// 使用方法: go test -run TestBenchmarkSingleService -v -timeout 10m
func TestBenchmarkSingleService(t *testing.T) {
	concurrency := 4
	requestCount := 20
	maxTokens := int32(128)
	temperature := float32(0.2)
	prompt := "Introduce yourself briefly."

	t.Logf("Starting benchmark for %s", serviceURL)
	t.Logf("Config: concurrency=%d, requests=%d, max_tokens=%d", concurrency, requestCount, maxTokens)

	result := runBenchmarkTest(t, concurrency, requestCount, maxTokens, temperature, prompt)
	printBenchmarkResult(t, result)
}

// TestBenchmarkHighLoad 高负载测试
// 使用方法: go test -run TestBenchmarkHighLoad -v -timeout 20m
func TestBenchmarkHighLoad(t *testing.T) {

	concurrency := 8
	requestCount := 50
	maxTokens := int32(256)
	temperature := float32(0.2)
	prompt := "Introduce yourself briefly."

	t.Logf("Starting high-load benchmark for %s", serviceURL)
	t.Logf("Config: concurrency=%d, requests=%d, max_tokens=%d", concurrency, requestCount, maxTokens)

	result := runBenchmarkTest(t, concurrency, requestCount, maxTokens, temperature, prompt)
	printBenchmarkResult(t, result)
}

// TestBenchmarkLongText 长文本生成测试
// 使用方法: go test -run TestBenchmarkLongText -v -timeout 20m
func TestBenchmarkLongText(t *testing.T) {

	concurrency := 2
	requestCount := 10
	maxTokens := int32(512)
	temperature := float32(0.2)
	prompt := "Write a detailed technical article about machine learning. Include key concepts, applications, and future trends."

	t.Logf("Starting long-text benchmark for %s", serviceURL)
	t.Logf("Config: concurrency=%d, requests=%d, max_tokens=%d", concurrency, requestCount, maxTokens)

	result := runBenchmarkTest(t, concurrency, requestCount, maxTokens, temperature, prompt)
	printBenchmarkResult(t, result)
}

// TestBenchmarkLowLatency 低延迟测试（短文本）
// 使用方法: go test -run TestBenchmarkLowLatency -v -timeout 10m
func TestBenchmarkLowLatency(t *testing.T) {

	concurrency := 10
	requestCount := 50
	maxTokens := int32(32)
	temperature := float32(0.2)
	prompt := "Hello"

	t.Logf("Starting low-latency benchmark for %s", serviceURL)
	t.Logf("Config: concurrency=%d, requests=%d, max_tokens=%d", concurrency, requestCount, maxTokens)

	result := runBenchmarkTest(t, concurrency, requestCount, maxTokens, temperature, prompt)
	printBenchmarkResult(t, result)
}

// TestBenchmarkStress 压力测试
// 使用方法: go test -run TestBenchmarkStress -v -timeout 30m
func TestBenchmarkStress(t *testing.T) {

	concurrency := 16
	requestCount := 100
	maxTokens := int32(128)
	temperature := float32(0.2)
	prompt := "Introduce yourself briefly."

	t.Logf("Starting stress benchmark for %s", serviceURL)
	t.Logf("Config: concurrency=%d, requests=%d, max_tokens=%d", concurrency, requestCount, maxTokens)

	result := runBenchmarkTest(t, concurrency, requestCount, maxTokens, temperature, prompt)
	printBenchmarkResult(t, result)
}

func runBenchmarkTest(t *testing.T, concurrency, requestCount int, maxTokens int32, temperature float32, prompt string) *BenchmarkResult {
	metrics := &BenchmarkMetrics{
		Latencies:   make([]time.Duration, 0, requestCount),
		TokenCounts: make([]int, 0, requestCount),
		Errors:      make([]string, 0),
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, concurrency)

	startTime := time.Now()

	for i := range requestCount {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			latency, tokenCount, err := makeTestRequest(maxTokens, temperature, prompt)

			mu.Lock()
			if err != nil {
				metrics.Errors = append(metrics.Errors, fmt.Sprintf("Request %d: %v", index, err))
				metrics.FailureCount++
			} else {
				metrics.Latencies = append(metrics.Latencies, latency)
				metrics.TokenCounts = append(metrics.TokenCounts, tokenCount)
				metrics.SuccessCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()
	metrics.TotalDuration = time.Since(startTime)

	return calculateBenchmarkResult(serviceURL, requestCount, metrics)
}

func makeTestRequest(maxTokens int32, temperature float32, prompt string) (time.Duration, int, error) {
	client := &http.Client{
		Timeout: 60 * time.Second,
	}

	reqBody := map[string]interface{}{
		"model":       "local",
		"stream":      false,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"messages": []map[string]string{
			{"role": "system", "content": "You are helpful."},
			{"role": "user", "content": prompt},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return 0, 0, fmt.Errorf("marshal request failed: %w", err)
	}

	req, err := http.NewRequest("POST", serviceURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return 0, 0, fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)

	if err != nil {
		return latency, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return latency, 0, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var respData map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&respData); err != nil {
		return latency, 0, fmt.Errorf("decode response failed: %w", err)
	}

	// Extract token count from response
	tokenCount := 0
	if usage, ok := respData["usage"].(map[string]interface{}); ok {
		if total, ok := usage["total_tokens"].(float64); ok {
			tokenCount = int(total)
		}
	}

	return latency, tokenCount, nil
}

func calculateBenchmarkResult(serviceURL string, totalRequests int, metrics *BenchmarkMetrics) *BenchmarkResult {
	result := &BenchmarkResult{
		TotalRequests:      totalRequests,
		SuccessfulRequests: metrics.SuccessCount,
		FailedRequests:     metrics.FailureCount,
		Duration:           metrics.TotalDuration,
	}

	if len(metrics.Latencies) == 0 {
		return result
	}

	// Calculate latency statistics
	sort.Slice(metrics.Latencies, func(i, j int) bool { return metrics.Latencies[i] < metrics.Latencies[j] })

	var totalLatency time.Duration
	var totalTokens int

	for i, latency := range metrics.Latencies {
		totalLatency += latency
		totalTokens += metrics.TokenCounts[i]
	}

	result.AvgLatency = totalLatency / time.Duration(len(metrics.Latencies))
	result.MinLatency = metrics.Latencies[0]
	result.MaxLatency = metrics.Latencies[len(metrics.Latencies)-1]
	result.P50Latency = metrics.Latencies[len(metrics.Latencies)/2]
	result.P95Latency = metrics.Latencies[int(float64(len(metrics.Latencies))*0.95)]
	result.P99Latency = metrics.Latencies[int(float64(len(metrics.Latencies))*0.99)]
	result.TotalTokens = totalTokens
	result.ThroughputReqSec = float64(metrics.SuccessCount) / metrics.TotalDuration.Seconds()

	if len(metrics.Latencies) > 0 {
		avgLatencySec := result.AvgLatency.Seconds()
		if avgLatencySec > 0 {
			avgTokensPerRequest := float64(totalTokens) / float64(len(metrics.Latencies))
			result.AvgTokensPerSec = avgTokensPerRequest / avgLatencySec
		}
	}

	return result
}

func printBenchmarkResult(t *testing.T, r *BenchmarkResult) {
	t.Logf("\n========== Benchmark Results ==========")
	t.Logf("Service URL:         %s", serviceURL)
	t.Logf("Total Requests:      %d", r.TotalRequests)
	t.Logf("Successful:          %d", r.SuccessfulRequests)
	t.Logf("Failed:              %d", r.FailedRequests)
	t.Logf("Duration:            %.2fs", r.Duration.Seconds())
	t.Logf("Success Rate:        %.1f%%", float64(r.SuccessfulRequests)/float64(r.TotalRequests)*100)

	if r.SuccessfulRequests == 0 {
		t.Logf("No successful requests to analyze")
		return
	}

	t.Logf("\n--- Latency (ms) ---")
	t.Logf("Min:                 %.2f", r.MinLatency.Seconds()*1000)
	t.Logf("Avg:                 %.2f", r.AvgLatency.Seconds()*1000)
	t.Logf("P50:                 %.2f", r.P50Latency.Seconds()*1000)
	t.Logf("P95:                 %.2f", r.P95Latency.Seconds()*1000)
	t.Logf("P99:                 %.2f", r.P99Latency.Seconds()*1000)
	t.Logf("Max:                 %.2f", r.MaxLatency.Seconds()*1000)

	t.Logf("\n--- Throughput ---")
	t.Logf("Requests/sec:        %.2f", r.ThroughputReqSec)
	t.Logf("Total Tokens:        %d", r.TotalTokens)
	t.Logf("Avg Tokens/Request:  %.0f", float64(r.TotalTokens)/float64(r.SuccessfulRequests))
	t.Logf("Avg Tokens/sec:      %.2f", r.AvgTokensPerSec)

	t.Logf("\n========================================")
}

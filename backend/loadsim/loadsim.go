package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

const pollInterval = 500 * time.Millisecond

type application struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Status   string `json:"status"`
}

type company struct {
	ID string `json:"id"`
}

type scenarioResult struct {
	number   int
	status   string
	stage    string
	duration time.Duration
	err      error
	timeout  bool
}

func main() {
	applications := flag.Int("applications", 10, "number of credit applications")
	workers := flag.Int("workers", 2, "number of concurrent scenarios")
	timeout := flag.Duration("timeout", 90*time.Second, "timeout for one scenario")
	flag.Parse()

	if *applications <= 0 || *workers <= 0 || *timeout <= 0 {
		fmt.Fprintln(os.Stderr, "applications, workers and timeout must be positive")
		os.Exit(1)
	}

	baseURL := strings.TrimRight(envOrDefault("LOADTEST_BASE_URL", "http://localhost:8080"), "/")
	email := envOrDefault("LOADTEST_EMAIL", "demo.ooo.good@lasagna.test")
	password := envOrDefault("LOADTEST_PASSWORD", "DemoCabinet2026!")
	client := &http.Client{Timeout: 15 * time.Second}

	token, err := login(context.Background(), client, baseURL, email, password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "login failed:", err)
		os.Exit(1)
	}

	companyID, err := loadCompanyID(context.Background(), client, baseURL, token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "company lookup failed:", err)
		os.Exit(1)
	}

	started := time.Now()
	results := runLoad(client, baseURL, token, companyID, *applications, *workers, *timeout)
	printReport(results, *applications, *workers, *timeout, time.Since(started))
	for _, result := range results {
		if result.err != nil {
			os.Exit(1)
		}
	}
}

func runLoad(client *http.Client, baseURL, token, companyID string, applications, workers int, timeout time.Duration) []scenarioResult {
	jobs := make(chan int)
	results := make(chan scenarioResult, applications)
	workerCount := min(workers, applications)
	runID := time.Now().UnixNano()

	var wg sync.WaitGroup
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for number := range jobs {
				ctx, cancel := context.WithTimeout(context.Background(), timeout)
				result := runScenario(ctx, client, baseURL, token, companyID, runID, number)
				cancel()
				results <- result
			}
		}()
	}

	go func() {
		for number := 1; number <= applications; number++ {
			jobs <- number
		}
		close(jobs)
		wg.Wait()
		close(results)
	}()

	collected := make([]scenarioResult, 0, applications)
	for result := range results {
		collected = append(collected, result)
	}
	sort.Slice(collected, func(i, j int) bool { return collected[i].number < collected[j].number })
	return collected
}

func runScenario(ctx context.Context, client *http.Client, baseURL, token, companyID string, runID int64, number int) scenarioResult {
	started := time.Now()
	result := scenarioResult{number: number}
	fail := func(stage string, err error) scenarioResult {
		result.stage = stage
		result.duration = time.Since(started)
		result.err = err
		result.timeout = ctx.Err() == context.DeadlineExceeded
		return result
	}
	key := func(stage string) string {
		return fmt.Sprintf("loadsim-%d-%d-%s", runID, number, stage)
	}

	var app application
	if err := doJSON(ctx, client, http.MethodPost, baseURL+"/api/v0/applications", token, key("create"), map[string]any{
		"company_id": companyID,
	}, &app); err != nil {
		return fail("create", err)
	}

	if err := doJSON(ctx, client, http.MethodPatch, baseURL+"/api/v0/applications/"+app.ID, token, "", map[string]any{
		"expected_revision": app.Revision,
		"request": map[string]any{
			"purpose":         "working_capital",
			"amount_kopecks":  100000000,
			"need_period":     map[string]any{"unit": "months", "value": 12},
			"product_code":    "express",
			"term_months":     12,
			"collateral_mode": "none",
		},
	}, &app); err != nil {
		return fail("patch", err)
	}

	if err := doJSON(ctx, client, http.MethodPost, baseURL+"/api/v0/applications/"+app.ID+"/financials/fetch", token, key("financials"), map[string]any{
		"expected_revision": app.Revision,
		"demo_mode":         "normal",
	}, &app); err != nil {
		return fail("financials", err)
	}

	if err := doJSON(ctx, client, http.MethodPost, baseURL+"/api/v0/applications/"+app.ID+"/submit", token, key("submit"), map[string]any{
		"expected_revision": app.Revision,
	}, &app); err != nil {
		return fail("submit", err)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		switch app.Status {
		case "manual_review", "rejected":
			result.status = app.Status
			result.duration = time.Since(started)
			return result
		}

		select {
		case <-ctx.Done():
			return fail("poll", ctx.Err())
		case <-ticker.C:
			if err := doJSON(ctx, client, http.MethodGet, baseURL+"/api/v0/applications/"+app.ID, token, "", nil, &app); err != nil {
				return fail("poll", err)
			}
		}
	}
}

func loadCompanyID(ctx context.Context, client *http.Client, baseURL, token string) (string, error) {
	var companies []company
	if err := doJSON(ctx, client, http.MethodGet, baseURL+"/api/v0/companies", token, "", nil, &companies); err != nil {
		return "", err
	}
	if len(companies) != 1 {
		return "", fmt.Errorf("expected exactly one company, got %d", len(companies))
	}
	return companies[0].ID, nil
}

func login(ctx context.Context, client *http.Client, baseURL, email, password string) (string, error) {
	var result struct {
		AccessToken string `json:"access_token"`
	}
	err := doJSON(ctx, client, http.MethodPost, baseURL+"/api/v0/auth/login", "", "", map[string]string{
		"email": email, "password": password,
	}, &result)
	if err != nil {
		return "", err
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("response does not contain access_token")
	}
	return result.AccessToken, nil
}

func doJSON(ctx context.Context, client *http.Client, method, url, token, idempotencyKey string, body, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	if out == nil {
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func printReport(results []scenarioResult, applications, workers int, timeout, elapsed time.Duration) {
	statuses := map[string]int{"manual_review": 0, "rejected": 0}
	var durations []time.Duration
	errorsCount, timeouts := 0, 0
	for _, result := range results {
		if result.err != nil {
			errorsCount++
			if result.timeout {
				timeouts++
			}
			continue
		}
		statuses[result.status]++
		durations = append(durations, result.duration)
	}

	fmt.Println("Pipeline load test")
	fmt.Printf("applications: %d\n", applications)
	fmt.Printf("workers: %d\n", min(workers, applications))
	fmt.Printf("scenario timeout: %s\n", timeout)
	fmt.Printf("elapsed: %s\n", elapsed.Round(time.Millisecond))
	fmt.Printf("manual_review: %d\n", statuses["manual_review"])
	fmt.Printf("rejected: %d\n", statuses["rejected"])
	fmt.Printf("errors: %d\n", errorsCount)
	fmt.Printf("timeouts: %d\n", timeouts)
	if elapsed > 0 {
		fmt.Printf("throughput: %.2f applications/s\n", float64(len(durations))/elapsed.Seconds())
	}
	if len(durations) > 0 {
		sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
		fmt.Printf("latency p50: %s\n", percentile(durations, 0.50).Round(time.Millisecond))
		fmt.Printf("latency p95: %s\n", percentile(durations, 0.95).Round(time.Millisecond))
		fmt.Printf("latency p99: %s\n", percentile(durations, 0.99).Round(time.Millisecond))
		fmt.Printf("latency max: %s\n", durations[len(durations)-1].Round(time.Millisecond))
	}
	for _, result := range results {
		if result.err != nil {
			fmt.Printf("scenario %d failed at %s after %s: %v\n", result.number, result.stage, result.duration.Round(time.Millisecond), result.err)
		}
	}
	if errorsCount == 0 {
		fmt.Println("verdict: PASS")
	} else {
		fmt.Println("verdict: FAIL")
	}
}

func percentile(sorted []time.Duration, quantile float64) time.Duration {
	index := int(math.Ceil(quantile*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	return sorted[index]
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

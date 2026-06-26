package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

type pingResult struct {
	URL        string
	StatusCode int
	Err        error
	Duration   time.Duration
}

func main() {
	urlsFlag := flag.String("urls", envOrDefault("KEEPALIVE_URLS", ""), "comma-separated service URLs to ping")
	interval := flag.Duration("interval", 5*time.Minute, "ping interval")
	timeout := flag.Duration("timeout", 20*time.Second, "per-request timeout")
	once := flag.Bool("once", false, "ping once and exit")
	flag.Parse()

	urls := parseURLs(*urlsFlag)
	if len(urls) == 0 {
		log.Fatal("no keepalive URLs configured; set KEEPALIVE_URLS or pass -urls")
	}

	client := &http.Client{Timeout: *timeout}

	for {
		results := pingAll(client, urls, *timeout)
		failed := false
		for _, result := range results {
			if result.Err != nil {
				failed = true
				log.Printf("FAIL %s: %v (%s)", result.URL, result.Err, result.Duration.Round(time.Millisecond))
				continue
			}
			if result.StatusCode < 200 || result.StatusCode >= 500 {
				failed = true
				log.Printf("FAIL %s: status %d (%s)", result.URL, result.StatusCode, result.Duration.Round(time.Millisecond))
				continue
			}
			log.Printf("OK   %s: status %d (%s)", result.URL, result.StatusCode, result.Duration.Round(time.Millisecond))
		}

		if *once {
			if failed {
				os.Exit(1)
			}
			return
		}

		time.Sleep(*interval)
	}
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func parseURLs(raw string) []string {
	parts := strings.Split(raw, ",")
	urls := make([]string, 0, len(parts))
	for _, part := range parts {
		u := strings.TrimSpace(part)
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			u = "https://" + u
		}
		u = strings.TrimRight(u, "/")
		if !strings.Contains(strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://"), "/") {
			u += "/healthz"
		}
		urls = append(urls, u)
	}
	return urls
}

func pingAll(client *http.Client, urls []string, timeout time.Duration) []pingResult {
	results := make([]pingResult, len(urls))
	var wg sync.WaitGroup
	wg.Add(len(urls))

	for i, u := range urls {
		go func(index int, target string) {
			defer wg.Done()
			results[index] = ping(client, target, timeout)
		}(i, u)
	}

	wg.Wait()
	return results
}

func ping(client *http.Client, target string, timeout time.Duration) pingResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusCode, err := doRequest(ctx, client, http.MethodHead, target)
	if err != nil || statusCode == http.StatusMethodNotAllowed {
		statusCode, err = doRequest(ctx, client, http.MethodGet, target)
	}

	return pingResult{
		URL:        target,
		StatusCode: statusCode,
		Err:        err,
		Duration:   time.Since(start),
	}
}

func doRequest(ctx context.Context, client *http.Client, method, target string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "minitrue-keepalive/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return resp.StatusCode, fmt.Errorf("server returned %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

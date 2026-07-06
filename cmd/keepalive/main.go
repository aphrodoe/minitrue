package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

type pingResult struct {
	InputURL     string
	EffectiveURL string
	StatusCode   int
	Err          error
	Duration     time.Duration
	Attempts     int
}

func main() {
	urlsFlag := flag.String("urls", envOrDefault("KEEPALIVE_URLS", ""), "comma-separated service URLs to ping")
	interval := flag.Duration("interval", 5*time.Minute, "ping interval")
	timeout := flag.Duration("timeout", 45*time.Second, "per-request timeout")
	retries := flag.Int("retries", 3, "number of attempts per URL")
	retryDelay := flag.Duration("retry-delay", 8*time.Second, "delay between retries")
	once := flag.Bool("once", false, "ping once and exit")
	allowFailures := flag.Bool("allow-failures", false, "exit with 0 even if some pings fail")
	flag.Parse()

	inputs := parseInputURLs(*urlsFlag)
	if len(inputs) == 0 {
		log.Fatal("no keepalive URLs configured; set KEEPALIVE_URLS or pass -urls")
	}
	if *retries < 1 {
		log.Fatal("-retries must be >= 1")
	}

	client := &http.Client{Timeout: *timeout}

	for {
		results := pingAll(client, inputs, *timeout, *retries, *retryDelay)
		failed := false

		for _, result := range results {
			if result.Err != nil {
				failed = true
				log.Printf("FAIL %s: %v (attempts=%d, last_url=%s, elapsed=%s)",
					result.InputURL,
					result.Err,
					result.Attempts,
					result.EffectiveURL,
					result.Duration.Round(time.Millisecond),
				)
				continue
			}

			if !isHealthyStatus(result.StatusCode) {
				failed = true
				log.Printf("FAIL %s: status %d (attempts=%d, url=%s, elapsed=%s)",
					result.InputURL,
					result.StatusCode,
					result.Attempts,
					result.EffectiveURL,
					result.Duration.Round(time.Millisecond),
				)
				continue
			}

			log.Printf("OK   %s: status %d (attempts=%d, url=%s, elapsed=%s)",
				result.InputURL,
				result.StatusCode,
				result.Attempts,
				result.EffectiveURL,
				result.Duration.Round(time.Millisecond),
			)
		}

		if *once {
			if failed && !*allowFailures {
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

func parseInputURLs(raw string) []string {
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
		urls = append(urls, u)
	}
	return urls
}

func pingAll(client *http.Client, inputs []string, timeout time.Duration, retries int, retryDelay time.Duration) []pingResult {
	results := make([]pingResult, len(inputs))
	var wg sync.WaitGroup
	wg.Add(len(inputs))

	for i, input := range inputs {
		go func(index int, target string) {
			defer wg.Done()
			results[index] = pingWithRetries(client, target, timeout, retries, retryDelay)
		}(i, input)
	}

	wg.Wait()
	return results
}

func pingWithRetries(client *http.Client, input string, timeout time.Duration, retries int, retryDelay time.Duration) pingResult {
	start := time.Now()
	candidates, buildErr := buildCandidateURLs(input)
	if buildErr != nil {
		return pingResult{InputURL: input, Err: buildErr, Duration: time.Since(start)}
	}

	last := pingResult{InputURL: input}
	for attempt := 1; attempt <= retries; attempt++ {
		for _, candidate := range candidates {
			result := pingOne(client, candidate, timeout)
			result.InputURL = input
			result.Attempts = attempt
			last = result

			if result.Err == nil && isHealthyStatus(result.StatusCode) {
				last.Duration = time.Since(start)
				return last
			}
		}

		if attempt < retries {
			time.Sleep(retryDelay)
		}
	}

	last.Duration = time.Since(start)
	if last.Err == nil && !isHealthyStatus(last.StatusCode) {
		last.Err = fmt.Errorf("unhealthy status %d", last.StatusCode)
	}
	if last.Err == nil {
		last.Err = errors.New("ping failed")
	}
	return last
}

func buildCandidateURLs(raw string) ([]string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL: %s", raw)
	}

	base := strings.TrimRight(u.Scheme+"://"+u.Host, "/")
	path := strings.TrimSpace(u.EscapedPath())
	query := u.RawQuery

	if path == "" || path == "/" {
		return []string{base + "/healthz", base + "/"}, nil
	}

	explicit := base + path
	if query != "" {
		explicit += "?" + query
	}

	if path == "/healthz" {
		return []string{explicit, base + "/"}, nil
	}

	return []string{explicit}, nil
}

func pingOne(client *http.Client, target string, timeout time.Duration) pingResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	statusCode, err := doRequest(ctx, client, http.MethodHead, target)
	if err != nil || statusCode == http.StatusMethodNotAllowed {
		statusCode, err = doRequest(ctx, client, http.MethodGet, target)
	}

	return pingResult{
		EffectiveURL: target,
		StatusCode:   statusCode,
		Err:          err,
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

func isHealthyStatus(statusCode int) bool {
	return statusCode >= 200 && statusCode < 500
}

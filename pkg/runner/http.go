package runner

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sbkg0002/kubernetes-testing-framework/pkg/config"
)

type httpRunner struct {
	tc config.TestCase
}

func newHTTPRunner(tc config.TestCase) *httpRunner {
	return &httpRunner{tc: tc}
}

func (r *httpRunner) Run(ctx context.Context) (Result, error) {
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.tc.URL, nil)
	if err != nil {
		return Result{Elapsed: time.Since(start)}, fmt.Errorf("building request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return Result{Elapsed: elapsed}, fmt.Errorf("executing request: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)

	statusOK := true
	if r.tc.Expect.Status != 0 {
		statusOK = resp.StatusCode == r.tc.Expect.Status
	}

	bodyOK := true
	if r.tc.Expect.Body != "" {
		bodyOK = strings.Contains(body, r.tc.Expect.Body)
	}

	passed := statusOK && bodyOK

	var output string
	if !passed {
		output = fmt.Sprintf("status %d (expected %d), body: %s",
			resp.StatusCode, r.tc.Expect.Status, truncate(body, 512))
	} else {
		output = fmt.Sprintf("status %d", resp.StatusCode)
	}

	return Result{Passed: passed, Output: output, Elapsed: elapsed}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

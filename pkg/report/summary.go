package report

import "time"

// Summarize computes aggregate pass/fail counts from a slice of results.
func Summarize(name string, results []TestResult, elapsed time.Duration) Summary {
	var passed, failed int
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	return Summary{
		Name:     name,
		Total:    len(results),
		Passed:   passed,
		Failed:   failed,
		Duration: elapsed,
		Results:  results,
	}
}

// AllPassed returns true if every result in the slice passed.
func AllPassed(results []TestResult) bool {
	for _, r := range results {
		if !r.Passed {
			return false
		}
	}
	return true
}

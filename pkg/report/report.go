package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// TestResult holds the outcome of one test case.
type TestResult struct {
	Name    string        `json:"name"`
	Passed  bool          `json:"passed"`
	Output  string        `json:"output,omitempty"`
	Elapsed time.Duration `json:"elapsed_ms"`
}

// Summary is the aggregate result of a test suite run.
type Summary struct {
	Name     string        `json:"name"`
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Failed   int           `json:"failed"`
	Duration time.Duration `json:"duration_ms"`
	Results  []TestResult  `json:"results"`
}

// Formatter writes a Summary to w.
type Formatter interface {
	Format(w io.Writer, s Summary) error
}

// New returns a Formatter for the given format name ("json" or "pretty").
// Unknown formats fall back to pretty.
func New(format string) Formatter {
	switch format {
	case "json":
		return &jsonFormatter{}
	default:
		return &prettyFormatter{}
	}
}

// --- JSON formatter ---

type jsonFormatter struct{}

func (f *jsonFormatter) Format(w io.Writer, s Summary) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// --- Pretty formatter ---

const (
	colorReset  = "\033[0m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
	colorBold   = "\033[1m"
)

type prettyFormatter struct{}

func (f *prettyFormatter) Format(w io.Writer, s Summary) error {
	useColor := isTerminal(w)

	green := func(s string) string {
		if useColor {
			return colorGreen + s + colorReset
		}
		return s
	}
	red := func(s string) string {
		if useColor {
			return colorRed + s + colorReset
		}
		return s
	}
	bold := func(s string) string {
		if useColor {
			return colorBold + s + colorReset
		}
		return s
	}

	fmt.Fprintf(w, "\n%s  %s\n", bold("Suite:"), s.Name)
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("─", 60))

	for _, r := range s.Results {
		status := green("PASS")
		if !r.Passed {
			status = red("FAIL")
		}
		fmt.Fprintf(w, "  %s  %-40s (%s)\n", status, r.Name, formatDuration(r.Elapsed))
		if !r.Passed && r.Output != "" {
			for _, line := range strings.Split(strings.TrimSpace(r.Output), "\n") {
				fmt.Fprintf(w, "        %s\n", line)
			}
		}
	}

	fmt.Fprintf(w, "\n%s\n", strings.Repeat("─", 60))

	summary := fmt.Sprintf("%d/%d passed", s.Passed, s.Total)
	if s.Failed == 0 {
		fmt.Fprintf(w, "  %s  %s  (%s)\n\n", green("OK"), summary, formatDuration(s.Duration))
	} else {
		fmt.Fprintf(w, "  %s  %s  (%s)\n\n", red("FAIL"), summary, formatDuration(s.Duration))
	}
	return nil
}

func isTerminal(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

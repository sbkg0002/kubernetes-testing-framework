package runner

import (
	"fmt"
	"time"

	"github.com/sbkg0002/kubernetes-testing-framework/pkg/config"

	"context"
)

// Result holds the outcome of a single test run.
type Result struct {
	Passed  bool
	Output  string
	Elapsed time.Duration
}

// Runner executes one test case and returns a Result.
type Runner interface {
	Run(ctx context.Context) (Result, error)
}

// Factory constructs a Runner from a TestCase.
type Factory func(tc config.TestCase) (Runner, error)

var registry = map[string]Factory{}

// Register adds a named factory to the global registry.
// Call from an init() function or explicitly before calling New.
func Register(name string, f Factory) {
	registry[name] = f
}

// New creates a Runner for the given TestCase using the registered factory.
func New(tc config.TestCase) (Runner, error) {
	f, ok := registry[tc.Runner]
	if !ok {
		return nil, fmt.Errorf("unknown runner %q (registered: %v)", tc.Runner, registeredNames())
	}
	return f(tc)
}

func registeredNames() []string {
	names := make([]string, 0, len(registry))
	for k := range registry {
		names = append(names, k)
	}
	return names
}

// Init registers all built-in runners. configDir is the directory containing
// the ktf.yaml file; it is used to resolve relative script paths.
func Init(configDir string) {
	Register("http", func(tc config.TestCase) (Runner, error) {
		return newHTTPRunner(tc), nil
	})
	Register("shell", func(tc config.TestCase) (Runner, error) {
		return newShellRunner(tc, configDir), nil
	})
}

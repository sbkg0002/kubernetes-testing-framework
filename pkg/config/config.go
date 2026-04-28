package config

import (
	"fmt"
	"os"
	"time"

	"sigs.k8s.io/yaml"
)

// Config is the top-level suite definition loaded from a ktf.yaml file.
type Config struct {
	Name      string        `json:"name"`
	Timeout   Duration      `json:"timeout"`
	Resources []ResourceRef `json:"resources"`
	Wait      WaitConfig    `json:"wait"`
	Tests     []TestCase    `json:"tests"`
	Teardown  string        `json:"teardown"` // always | on-success | never
}

// ResourceRef describes a resource to deploy before running tests.
type ResourceRef struct {
	Type      string `json:"type"`      // manifest | helm
	Path      string `json:"path"`      // manifest: local directory or file path
	URL       string `json:"url"`       // manifest: remote URL (alternative to path)
	Chart     string `json:"chart"`     // helm: chart path
	Values    string `json:"values"`    // helm: values file path
	Name      string `json:"name"`      // helm: release name (defaults to chart dir name)
	Namespace string `json:"namespace"` // optional namespace override
}

// WaitConfig controls how ktf polls for resource readiness.
type WaitConfig struct {
	Min      Duration `json:"min"`
	Max      Duration `json:"max"`
	Strategy string   `json:"strategy"` // backoff | fixed | linear
}

// TestCase describes a single test.
type TestCase struct {
	Name string `json:"name"`
	// runner selects the implementation: "http" or "shell"
	Runner string `json:"runner"`
	// http runner fields
	URL    string       `json:"url"`
	Expect ExpectConfig `json:"expect"`
	// shell runner fields
	Script string            `json:"script"` // path to script (relative to config file)
	Inline string            `json:"inline"` // inline shell commands
	Env    map[string]string `json:"env"`
}

// ExpectConfig holds expectations for the HTTP runner.
type ExpectConfig struct {
	Status int    `json:"status"`
	Body   string `json:"body"` // optional substring match against response body
}

// Load reads and parses a ktf.yaml file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %q: %w", path, err)
	}
	applyDefaults(&cfg)
	return &cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Teardown == "" {
		cfg.Teardown = "always"
	}
	if cfg.Wait.Strategy == "" {
		cfg.Wait.Strategy = "backoff"
	}
	if cfg.Timeout.Duration == 0 {
		cfg.Timeout.Duration = 10 * time.Minute
	}
	if cfg.Wait.Min.Duration == 0 {
		cfg.Wait.Min.Duration = 5 * time.Second
	}
	if cfg.Wait.Max.Duration == 0 {
		cfg.Wait.Max.Duration = 2 * time.Minute
	}
}

// Validate performs semantic checks on a loaded config.
func Validate(cfg *Config) error {
	if cfg.Name == "" {
		return fmt.Errorf("config.name is required")
	}
	if cfg.Timeout.Duration <= 0 {
		return fmt.Errorf("config.timeout must be positive")
	}
	if len(cfg.Tests) == 0 {
		return fmt.Errorf("at least one test must be defined")
	}
	for i, t := range cfg.Tests {
		if t.Name == "" {
			return fmt.Errorf("tests[%d].name is required", i)
		}
		if t.Runner == "" {
			return fmt.Errorf("tests[%d] (%q): runner is required", i, t.Name)
		}
	}
	switch cfg.Teardown {
	case "always", "on-success", "never":
	default:
		return fmt.Errorf("teardown must be one of: always, on-success, never (got %q)", cfg.Teardown)
	}
	return nil
}

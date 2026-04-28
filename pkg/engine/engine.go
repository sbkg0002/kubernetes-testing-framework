package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sync"
	"time"

	"github.com/sbkg0002/kubernetes-testing-framework/pkg/config"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/poller"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/report"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/resource"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/runner"
)

// Options configures Engine behaviour.
type Options struct {
	// Kubeconfig is the path to a kubeconfig file.
	// Empty string → in-cluster → $KUBECONFIG / ~/.kube/config.
	Kubeconfig string
	// Namespace is the default Kubernetes namespace.
	Namespace string
	// Parallel runs all test cases concurrently when true.
	Parallel bool
	// ConfigPath is the path to the ktf.yaml file. Used to resolve relative
	// script paths in shell runner test cases.
	ConfigPath string
}

// Engine orchestrates the deploy → poll → test → teardown lifecycle.
type Engine struct {
	cfg     *config.Config
	manager resource.Manager
	poller  poller.Poller
	opts    Options
	log     *slog.Logger
}

// New builds an Engine from a loaded Config and Options.
func New(cfg *config.Config, opts Options) (*Engine, error) {
	log := slog.Default()

	restCfg, err := resource.BuildRestConfig(opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("building REST config: %w", err)
	}

	mgr, err := resource.NewManager(restCfg, opts.Namespace, log)
	if err != nil {
		return nil, fmt.Errorf("creating resource manager: %w", err)
	}

	p := poller.New(poller.Options{
		Min:      cfg.Wait.Min.Duration,
		Max:      cfg.Wait.Max.Duration,
		Strategy: poller.StrategyFromString(cfg.Wait.Strategy),
		Jitter:   true,
	})

	configDir := filepath.Dir(opts.ConfigPath)
	runner.Init(configDir)

	return &Engine{
		cfg:     cfg,
		manager: mgr,
		poller:  p,
		opts:    opts,
		log:     log,
	}, nil
}

// Run executes the full suite lifecycle and returns per-test results.
func (e *Engine) Run(ctx context.Context) ([]report.TestResult, error) {
	resources := e.convertResources()

	// Phase 1: Apply resources
	e.log.Info("phase: apply", "resources", len(resources))
	if err := e.manager.Apply(ctx, resources); err != nil {
		e.runTeardown(resources, false)
		return nil, fmt.Errorf("applying resources: %w", err)
	}

	// Phase 2: Wait for readiness
	if len(resources) > 0 {
		e.log.Info("phase: wait-ready", "strategy", e.cfg.Wait.Strategy,
			"min", e.cfg.Wait.Min.Duration, "max", e.cfg.Wait.Max.Duration)

		waitCtx, waitCancel := context.WithTimeout(ctx, e.cfg.Timeout.Duration)
		defer waitCancel()

		err := e.poller.Poll(waitCtx, func(ctx context.Context) (bool, error) {
			if err := e.manager.WaitReady(ctx, resources); err != nil {
				if errors.Is(err, resource.ErrNotReady) {
					return false, nil
				}
				return false, err
			}
			return true, nil
		})
		if err != nil {
			e.runTeardown(resources, false)
			return nil, fmt.Errorf("waiting for readiness: %w", err)
		}
	}

	// Phase 3: Run tests
	e.log.Info("phase: test", "tests", len(e.cfg.Tests), "parallel", e.opts.Parallel)
	start := time.Now()
	var results []report.TestResult
	if e.opts.Parallel {
		results = e.runParallel(ctx)
	} else {
		results = e.runSequential(ctx)
	}
	_ = start

	// Phase 4: Teardown
	e.runTeardown(resources, report.AllPassed(results))
	return results, nil
}

func (e *Engine) runSequential(ctx context.Context) []report.TestResult {
	results := make([]report.TestResult, len(e.cfg.Tests))
	for i, tc := range e.cfg.Tests {
		if ctx.Err() != nil {
			results[i] = report.TestResult{Name: tc.Name, Passed: false, Output: "context cancelled"}
			continue
		}
		results[i] = e.execTest(ctx, tc)
		e.log.Info("test", "name", tc.Name, "passed", results[i].Passed, "elapsed", results[i].Elapsed)
	}
	return results
}

func (e *Engine) runParallel(ctx context.Context) []report.TestResult {
	results := make([]report.TestResult, len(e.cfg.Tests))
	var wg sync.WaitGroup
	for i, tc := range e.cfg.Tests {
		i, tc := i, tc
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = e.execTest(ctx, tc)
			e.log.Info("test", "name", tc.Name, "passed", results[i].Passed, "elapsed", results[i].Elapsed)
		}()
	}
	wg.Wait()
	return results
}

func (e *Engine) execTest(ctx context.Context, tc config.TestCase) report.TestResult {
	r, err := runner.New(tc)
	if err != nil {
		return report.TestResult{Name: tc.Name, Passed: false, Output: fmt.Sprintf("runner init: %v", err)}
	}
	result, err := r.Run(ctx)
	if err != nil {
		return report.TestResult{Name: tc.Name, Passed: false, Output: err.Error(), Elapsed: result.Elapsed}
	}
	return report.TestResult{Name: tc.Name, Passed: result.Passed, Output: result.Output, Elapsed: result.Elapsed}
}

func (e *Engine) runTeardown(resources []resource.Resource, allPassed bool) {
	switch e.cfg.Teardown {
	case "never":
		return
	case "on-success":
		if !allPassed {
			return
		}
	}
	e.log.Info("phase: teardown")
	teardownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := e.manager.Delete(teardownCtx, resources); err != nil {
		e.log.Error("teardown error", "error", err)
	}
}

// convertResources maps config.ResourceRef slices to resource.Resource slices.
func (e *Engine) convertResources() []resource.Resource {
	result := make([]resource.Resource, 0, len(e.cfg.Resources))
	configDir := filepath.Dir(e.opts.ConfigPath)
	for _, ref := range e.cfg.Resources {
		r := resource.Resource{
			Kind:      ref.Type,
			Name:      ref.Name,
			Namespace: ref.Namespace,
		}
		switch ref.Type {
		case "manifest":
			if ref.URL != "" {
				r.ManifestURL = ref.URL
			} else {
				p := ref.Path
				if !filepath.IsAbs(p) {
					p = filepath.Join(configDir, p)
				}
				r.ManifestPath = p
			}
		case "helm":
			chart := ref.Chart
			if !filepath.IsAbs(chart) {
				chart = filepath.Join(configDir, chart)
			}
			vals := ref.Values
			if vals != "" && !filepath.IsAbs(vals) {
				vals = filepath.Join(configDir, vals)
			}
			r.HelmChart = chart
			r.HelmValues = vals
			if r.Name == "" {
				r.Name = filepath.Base(chart)
			}
		}
		result = append(result, r)
	}
	return result
}

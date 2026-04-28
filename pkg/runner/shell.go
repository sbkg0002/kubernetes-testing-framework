package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/sbkg0002/kubernetes-testing-framework/pkg/config"
)

type shellRunner struct {
	tc        config.TestCase
	configDir string
}

func newShellRunner(tc config.TestCase, configDir string) *shellRunner {
	return &shellRunner{tc: tc, configDir: configDir}
}

func (r *shellRunner) Run(ctx context.Context) (Result, error) {
	start := time.Now()

	var cmd *exec.Cmd
	if r.tc.Inline != "" {
		cmd = exec.CommandContext(ctx, "bash", "-c", r.tc.Inline)
	} else if r.tc.Script != "" {
		scriptPath := r.tc.Script
		if !filepath.IsAbs(scriptPath) {
			scriptPath = filepath.Join(r.configDir, scriptPath)
		}
		cmd = exec.CommandContext(ctx, "bash", scriptPath)
	} else {
		return Result{Elapsed: time.Since(start)}, fmt.Errorf("shell runner requires either 'inline' or 'script'")
	}

	// Merge test env vars on top of the process environment
	cmd.Env = os.Environ()
	for k, v := range r.tc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	output := string(out)

	if err != nil {
		if ctx.Err() != nil {
			return Result{Elapsed: elapsed, Output: output}, fmt.Errorf("context cancelled: %w", ctx.Err())
		}
		// Non-zero exit: test failed, not a runner error
		return Result{Passed: false, Output: fmt.Sprintf("exit: %v\n%s", err, output), Elapsed: elapsed}, nil
	}

	return Result{Passed: true, Output: output, Elapsed: elapsed}, nil
}

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/sbkg0002/kubernetes-testing-framework/pkg/config"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/engine"
	"github.com/sbkg0002/kubernetes-testing-framework/pkg/report"
)

var rootCmd = &cobra.Command{
	Use:   "ktf",
	Short: "Kubernetes Testing Framework — deploy, wait, test, teardown",
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().String("config", "ktf.yaml", "path to suite config file")
	rootCmd.PersistentFlags().String("kubeconfig", "", "path to kubeconfig (default: in-cluster → $KUBECONFIG → ~/.kube/config)")
	rootCmd.PersistentFlags().String("namespace", "default", "default Kubernetes namespace")
	rootCmd.PersistentFlags().String("output", "pretty", "output format: pretty | json")
	rootCmd.PersistentFlags().Bool("parallel", false, "run tests in parallel")

	viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
	viper.BindPFlag("kubeconfig", rootCmd.PersistentFlags().Lookup("kubeconfig"))
	viper.BindPFlag("namespace", rootCmd.PersistentFlags().Lookup("namespace"))
	viper.BindPFlag("output", rootCmd.PersistentFlags().Lookup("output"))
	viper.BindPFlag("parallel", rootCmd.PersistentFlags().Lookup("parallel"))

	viper.SetEnvPrefix("KTF")
	viper.AutomaticEnv()

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(validateCmd)
}

// runCmd executes a test suite.
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Execute a ktf test suite",
	RunE:  runSuite,
}

func runSuite(cmd *cobra.Command, args []string) error {
	configPath := viper.GetString("config")
	if !filepath.IsAbs(configPath) {
		wd, _ := os.Getwd()
		configPath = filepath.Join(wd, configPath)
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	opts := engine.Options{
		Kubeconfig: viper.GetString("kubeconfig"),
		Namespace:  viper.GetString("namespace"),
		Parallel:   viper.GetBool("parallel"),
		ConfigPath: configPath,
	}

	eng, err := engine.New(cfg, opts)
	if err != nil {
		return fmt.Errorf("initialising engine: %w", err)
	}

	ctx := cmd.Context()
	start := time.Now()
	results, err := eng.Run(ctx)
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("suite failed: %w", err)
	}

	summary := report.Summarize(cfg.Name, results, elapsed)
	formatter := report.New(viper.GetString("output"))
	if err := formatter.Format(os.Stdout, summary); err != nil {
		return err
	}

	if summary.Failed > 0 {
		os.Exit(1)
	}
	return nil
}

// validateCmd checks the config file without connecting to a cluster.
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a ktf config file without running tests",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := viper.GetString("config")
		if !filepath.IsAbs(configPath) {
			wd, _ := os.Getwd()
			configPath = filepath.Join(wd, configPath)
		}

		cfg, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}
		if err := config.Validate(cfg); err != nil {
			return fmt.Errorf("invalid config: %w", err)
		}

		fmt.Printf("config %q is valid (%d resources, %d tests)\n",
			configPath, len(cfg.Resources), len(cfg.Tests))
		return nil
	},
}

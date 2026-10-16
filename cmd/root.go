package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger *zap.Logger
	// apiBaseURL is the controller REST API base (for CLI commands that call into a running gitopsctl start).
	apiBaseURL string
	apiToken   string
	// Optional integration event sinks shared by start + mutating CLI commands.
	eventsFile           string
	eventsWebhookURL     string
	eventsWebhookBearer  string
	eventsWebhookSecret  string
	eventsWebhookRetry   int
	eventsWebhookBackoff time.Duration
	eventsWebhookTimeout time.Duration
)

var rootCmd = &cobra.Command{
	Use:   "gitopsctl",
	Short: "A lightweight GitOps controller for Kubernetes",
	Long: `gitopsctl is a minimalistic, self-hosted GitOps controller that watches Git repositories
and applies Kubernetes manifests to target clusters.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize Zap logger with premium terminal aesthetics
		config := zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("15:04:05")
		config.EncoderConfig.CallerKey = "" // Remove caller for cleaner CLI output
		config.DisableStacktrace = true

		var err error
		logger, err = config.Build()
		if err != nil {
			return fmt.Errorf("failed to initialize logger: %w", err)
		}
		zap.ReplaceGlobals(logger)
		if apiToken == "" {
			apiToken = os.Getenv("GITOPSCTL_API_TOKEN")
		}
		return nil
	},
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if logger != nil {
			logger.Error("Command execution failed", zap.Error(err))
		} else {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		}
		os.Exit(1)
	}
}

// Logger returns the global zap logger instance.
func Logger() *zap.Logger {
	return logger
}

func RootCmd() *cobra.Command {
	return rootCmd
}

func init() {
	rootCmd.AddGroup(appGroup)
	rootCmd.AddGroup(clusterGroup)
	rootCmd.PersistentFlags().StringVar(&apiBaseURL, "api-url", "http://127.0.0.1:8080", "Base URL of the gitopsctl API (for commands that require a running controller)")
	rootCmd.PersistentFlags().StringVar(&apiToken, "api-token", "", "Bearer token for the gitopsctl API (defaults to $GITOPSCTL_API_TOKEN); with 'start' it is accepted as an admin token")
	rootCmd.PersistentFlags().StringVar(&eventsFile, "events-file", "", "Append integration events as JSON lines to this file (optional)")
	rootCmd.PersistentFlags().StringVar(&eventsWebhookURL, "events-webhook", "", "POST each integration event as JSON to this URL (optional)")
	rootCmd.PersistentFlags().StringVar(&eventsWebhookBearer, "events-webhook-bearer", "", "Bearer token for --events-webhook Authorization header (optional)")
	rootCmd.PersistentFlags().StringVar(&eventsWebhookSecret, "events-webhook-secret", "", "HMAC secret for X-GitOpsctl-Signature (sha256) header (optional)")
	rootCmd.PersistentFlags().IntVar(&eventsWebhookRetry, "events-webhook-retries", 2, "Number of webhook retry attempts for transient failures")
	rootCmd.PersistentFlags().DurationVar(&eventsWebhookBackoff, "events-webhook-backoff", 750*time.Millisecond, "Base backoff duration between webhook retries")
	rootCmd.PersistentFlags().DurationVar(&eventsWebhookTimeout, "events-webhook-timeout", 12*time.Second, "HTTP timeout per webhook request")
	rootCmd.AddCommand(startCmd)
}

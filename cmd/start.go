package cmd

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"aeswibon.com/github/gitopsctl/internal/api"
	"aeswibon.com/github/gitopsctl/internal/controller"
	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/events"
	"aeswibon.com/github/gitopsctl/internal/session"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	apiAddress     string
	apiTokenFile   string
	apiCORSOrigins []string
	agentsFile     string
	sessionsFile   string
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the GitOps controller and API server",
	Long: `Starts the GitOps controller, which continuously watches registered Git repositories and applies manifests to Kubernetes clusters,
and serves a REST API for automation.

Phase 2: optionally emit integration events (JSONL file and/or HTTP webhook) for external dashboards.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		apps, err := app.LoadApplications(app.DefaultAppConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load applications: %w", err)
		}

		clusters, err := cluster.LoadClusters(cluster.DefaultClusterConfigFile)
		if err != nil {
			return fmt.Errorf("failed to load clusters: %w", err)
		}

		if len(apps.List()) == 0 {
			logger.Warn("No applications registered. Use 'gitopsctl register-apps' to add an application.")
		}

		if len(clusters.List()) == 0 {
			logger.Warn("No clusters registered. Use 'gitopsctl register-cluster' to add a cluster.")
		}

		var ctrlOpts []controller.Option
		var sinks []events.Sink
		streamSink := events.NewStreamSink()
		historySink := events.NewHistorySink(100)
		sinks = append(sinks, streamSink, historySink) // Available for live stream and history.
		if eventsFile != "" {
			fs, err := events.NewFileSink(eventsFile)
			if err != nil {
				return fmt.Errorf("events file sink: %w", err)
			}
			sinks = append(sinks, fs)
		}
		if eventsWebhookURL != "" {
			sinks = append(sinks, events.NewWebhookSinkWithOptions(eventsWebhookURL, eventsWebhookBearer, events.WebhookOptions{
				Timeout:       eventsWebhookTimeout,
				Retries:       eventsWebhookRetry,
				RetryBackoff:  eventsWebhookBackoff,
				SigningSecret: eventsWebhookSecret,
			}))
		}
		bus := events.NewBus(logger, "gitopsctl-controller", sinks...)
		ctrlOpts = append(ctrlOpts, controller.WithEmitter(bus))
		logger.Info("Integration events enabled",
			zap.Int("sinks", len(sinks)),
			zap.Bool("sse_stream", true),
			zap.Bool("jsonl_sink", eventsFile != ""),
			zap.Bool("webhook_sink", eventsWebhookURL != ""))

		tokens, err := loadAPITokens()
		if err != nil {
			return err
		}
		listenAddress, err := resolveListenAddress(apiAddress, len(tokens) > 0)
		if err != nil {
			return err
		}
		if listenAddress != apiAddress {
			logger.Warn("No API token configured; binding API to loopback only. Set --api-token or --api-token-file to listen on other interfaces.", zap.String("address", listenAddress))
		}

		agentTokens, err := loadAgentTokens()
		if err != nil {
			return err
		}
		serverOpts := []api.ServerOption{api.WithAPITokens(tokens), api.WithAgentTokens(agentTokens), api.WithCORSOrigins(apiCORSOrigins)}
		var sessionManager *session.Manager
		if agentsFile != "" {
			sessionManager, err = newSessionManager(clusters)
			if err != nil {
				return err
			}
			serverOpts = append(serverOpts, api.WithSessions(sessionManager))
		}

		ctrl := controller.NewController(logger, apps, clusters, ctrlOpts...)
		apiServer := api.NewServer(logger, apps, clusters, ctrl, streamSink, historySink, serverOpts...)

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

		go func() {
			if err := ctrl.Start(app.DefaultAppConfigFile); err != nil {
				logger.Fatal("Failed to start controller", zap.Error(err))
			}
		}()

		janitorCtx, stopJanitor := context.WithCancel(context.Background())
		defer stopJanitor()
		if sessionManager != nil {
			go sessionManager.Run(janitorCtx, sessionReapInterval)
		}

		go func() {
			if err := apiServer.Start(listenAddress); err != nil && err != http.ErrServerClosed {
				logger.Fatal("Failed to start API server", zap.Error(err))
			}
		}()

		// Wait for an interrupt signal
		<-sigChan
		logger.Info("Received shutdown signal. Stopping controller...")

		timeoutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := apiServer.Stop(timeoutCtx); err != nil {
			logger.Error("API server shutdown error", zap.Error(err))
		}
		stopJanitor()
		ctrl.Stop()

		logger.Info("Controller stopped gracefully.")
		return nil
	},
}

func init() {
	startCmd.Flags().StringVarP(&apiAddress, "api-address", "a", ":8080", "Address for the API server to listen on (e.g., :8080, 0.0.0.0:8080)")
	startCmd.Flags().StringVar(&apiTokenFile, "api-token-file", "", "File with one \"<scope> <token>\" per line (scopes: read, sync, admin) for API authentication")
	startCmd.Flags().StringVar(&agentsFile, "agents-file", "", "YAML file with the agent policy and agent definitions; enables the /api/v1/sessions API")
	startCmd.Flags().StringVar(&sessionsFile, "sessions-file", "configs/sessions.json", "File where agent sessions are persisted (used with --agents-file)")
	startCmd.Flags().StringSliceVar(&apiCORSOrigins, "api-cors-origin", nil, "Browser origin allowed to call the API (repeatable); CORS is disabled by default")
}

func loadAPITokens() (map[string]api.Scope, error) {
	tokens := map[string]api.Scope{}
	if apiToken != "" {
		tokens[apiToken] = api.ScopeAdmin
	}
	if apiTokenFile == "" {
		return tokens, nil
	}
	f, err := os.Open(apiTokenFile)
	if err != nil {
		return nil, fmt.Errorf("open api token file: %w", err)
	}
	defer func() { _ = f.Close() }()
	fileTokens, err := api.ParseTokens(f)
	if err != nil {
		return nil, fmt.Errorf("parse api token file: %w", err)
	}
	for token, scope := range fileTokens {
		tokens[token] = scope
	}
	return tokens, nil
}

func resolveListenAddress(address string, authEnabled bool) (string, error) {
	if authEnabled {
		return address, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "", fmt.Errorf("invalid --api-address %q: %w", address, err)
	}
	if ip := net.ParseIP(host); host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return address, nil
	}
	return net.JoinHostPort("127.0.0.1", port), nil
}

const sessionReapInterval = time.Minute

func loadAgentTokens() (map[string]string, error) {
	if apiTokenFile == "" {
		return nil, nil
	}
	f, err := os.Open(apiTokenFile)
	if err != nil {
		return nil, fmt.Errorf("open api token file: %w", err)
	}
	defer func() { _ = f.Close() }()
	agentTokens, err := api.ParseAgentTokens(f)
	if err != nil {
		return nil, fmt.Errorf("parse api token file: %w", err)
	}
	return agentTokens, nil
}

func newSessionManager(clusters *cluster.Clusters) (*session.Manager, error) {
	cfg, err := session.LoadConfig(agentsFile)
	if err != nil {
		return nil, err
	}
	clusterFor := func(name string) (session.Cluster, error) {
		clusters.RLock()
		defer clusters.RUnlock()
		cl, ok := clusters.Get(name)
		if !ok {
			return nil, fmt.Errorf("cluster %q is not registered", name)
		}
		return session.NewK8sCluster(logger, cl.KubeconfigPath, cfg.Policy), nil
	}
	return session.NewManager(logger, cfg, clusterFor, sessionsFile)
}

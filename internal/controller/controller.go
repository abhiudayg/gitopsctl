package controller

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"aeswibon.com/github/gitopsctl/internal/core/app"
	"aeswibon.com/github/gitopsctl/internal/core/cluster"
	"aeswibon.com/github/gitopsctl/internal/core/git"
	"aeswibon.com/github/gitopsctl/internal/core/k8s"
	"aeswibon.com/github/gitopsctl/internal/core/notifications"
	"aeswibon.com/github/gitopsctl/internal/events"
	"aeswibon.com/github/gitopsctl/internal/metrics"
	"go.uber.org/zap"
)

type ClusterCommandType string

type ClusterCommand struct {
	Type        ClusterCommandType
	ClusterName string
}

const (
	ClusterCommandCheck    ClusterCommandType = "CHECK"
	MaxConsecutiveFailures                    = 5
	BaseBackoffDuration                       = 5 * time.Second
	GitOperationTimeout                       = 60 * time.Second
	K8sApplyTimeout                           = 120 * time.Second
	K8sConnectTimeout                         = 10 * time.Second
	ConfigWatchInterval                       = 5 * time.Second
)

type AppCommandType string

const (
	AppCommandStart   AppCommandType = "START"
	AppCommandStop    AppCommandType = "STOP"
	AppCommandSync    AppCommandType = "SYNC"
	AppCommandApprove AppCommandType = "APPROVE"
)

type AppCommand struct {
	Type    AppCommandType
	AppName string
	Data    map[string]any
}

type appRuntime struct {
	cancel   context.CancelFunc
	syncChan chan struct{}
}

type Controller struct {
	logger             *zap.Logger
	apps               *app.Applications
	clusters           *cluster.Clusters
	ctx                context.Context
	cancel             context.CancelFunc
	appCommandChan     chan AppCommand
	clusterCommandChan chan ClusterCommand
	runningApps        map[string]*appRuntime
	mu                 sync.Mutex
	wg                 sync.WaitGroup
	emitter            events.Emitter
	appConfigPath      string
	stopOnce           sync.Once
}

type Option func(*Controller)

func WithEmitter(e events.Emitter) Option {
	return func(c *Controller) {
		c.emitter = e
	}
}

func NewController(logger *zap.Logger, apps *app.Applications, clusters *cluster.Clusters, opts ...Option) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	ctrl := &Controller{
		logger:             logger,
		apps:               apps,
		clusters:           clusters,
		ctx:                ctx,
		cancel:             cancel,
		appCommandChan:     make(chan AppCommand, 50),
		clusterCommandChan: make(chan ClusterCommand, 50),
		runningApps:        make(map[string]*appRuntime),
	}
	for _, o := range opts {
		o(ctrl)
	}
	return ctrl
}

func (c *Controller) emit(ctx context.Context, typ events.Type, data map[string]any) {
	if c.emitter == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	c.emitter.Emit(ctx, typ, data)
}

func (c *Controller) Emit(typ events.Type, data map[string]any) {
	c.emit(context.Background(), typ, data)
}

func (c *Controller) Start(appConfigFile string) error {
	c.appConfigPath = appConfigFile
	c.logger.Info("Starting GitOps controller...")

	c.wg.Add(1)
	go c.commandDispatcher(appConfigFile)

	c.wg.Add(1)
	go c.clusterHealthChecker()

	c.wg.Add(1)
	go c.configWatcher(appConfigFile)

	c.apps.RLock()
	appsToStart := c.apps.List()
	c.apps.RUnlock()

	for _, application := range appsToStart {
		c.sendApp(AppCommand{Type: AppCommandStart, AppName: application.Name})
	}

	c.clusters.RLock()
	clustersToCheck := c.clusters.List()
	c.clusters.RUnlock()

	for _, cl := range clustersToCheck {
		c.sendCluster(ClusterCommand{Type: ClusterCommandCheck, ClusterName: cl.Name})
	}

	c.logger.Info("Initial reconciliation dispatched.")
	c.emit(c.ctx, events.TypeControllerStarted, map[string]any{
		"applications": len(appsToStart),
		"clusters":     len(clustersToCheck),
	})
	return nil
}

func (c *Controller) Stop() {
	c.stopOnce.Do(func() {
		c.emit(context.Background(), events.TypeControllerStopping, map[string]any{})
		c.logger.Info("Stopping GitOps controller...")
		c.cancel()
		c.wg.Wait()
		c.logger.Info("GitOps controller stopped.")
	})
}

func (c *Controller) sendApp(cmd AppCommand) {
	select {
	case c.appCommandChan <- cmd:
	case <-c.ctx.Done():
	}
}

func (c *Controller) sendCluster(cmd ClusterCommand) {
	select {
	case c.clusterCommandChan <- cmd:
	case <-c.ctx.Done():
	}
}

func (c *Controller) configWatcher(filePath string) {
	defer c.wg.Done()
	ticker := time.NewTicker(ConfigWatchInterval)
	defer ticker.Stop()

	var lastMod time.Time
	if info, err := os.Stat(filePath); err == nil {
		lastMod = info.ModTime()
	}

	for {
		select {
		case <-ticker.C:
			info, err := os.Stat(filePath)
			if err != nil {
				continue
			}
			if info.ModTime().After(lastMod) {
				c.logger.Info("Configuration change detected, reloading...", zap.String("file", filePath))
				lastMod = info.ModTime()
				c.reloadApplications(filePath)
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Controller) reloadApplications(filePath string) {
	newApps, err := app.LoadApplications(filePath)
	if err != nil {
		c.logger.Error("Failed to reload applications", zap.Error(err))
		return
	}

	c.apps.Lock()
	defer c.apps.Unlock()

	for _, a := range newApps.List() {
		existing, ok := c.apps.Get(a.Name)
		if !ok || existing.RepoURL != a.RepoURL || existing.Branch != a.Branch || existing.Path != a.Path || existing.ClusterName != a.ClusterName || existing.Interval != a.Interval {
			c.logger.Info("Detected new or updated application configuration", zap.String("app", a.Name))
			c.apps.Add(a)
			c.sendApp(AppCommand{Type: AppCommandStart, AppName: a.Name})
		}
	}

	currentNames := make(map[string]bool)
	for _, a := range newApps.List() {
		currentNames[a.Name] = true
	}
	for name := range c.apps.Apps {
		if !currentNames[name] {
			c.logger.Info("Detected application removal", zap.String("app", name))
			c.sendApp(AppCommand{Type: AppCommandStop, AppName: name})
			delete(c.apps.Apps, name)
		}
	}
}

func (c *Controller) StartApp(appName string) {
	c.sendApp(AppCommand{Type: AppCommandStart, AppName: appName})
}

func (c *Controller) StopApp(appName string) {
	c.sendApp(AppCommand{Type: AppCommandStop, AppName: appName})
}

func (c *Controller) TriggerSync(appName string) {
	c.sendApp(AppCommand{Type: AppCommandSync, AppName: appName})
}

func (c *Controller) ApproveSync(appName string, commitHash string) {
	c.sendApp(AppCommand{
		Type:    AppCommandApprove,
		AppName: appName,
		Data:    map[string]any{"commitHash": commitHash},
	})
}

func (c *Controller) TriggerClusterHealthCheck(clusterName string) {
	c.sendCluster(ClusterCommand{Type: ClusterCommandCheck, ClusterName: clusterName})
}

func (c *Controller) commandDispatcher(appConfigFile string) {
	defer c.wg.Done()
	c.logger.Info("Starting controller command dispatcher...")

	for {
		select {
		case cmd, ok := <-c.appCommandChan:
			if !ok {
				c.stopAllAppGoroutines()
				return
			}
			c.handleAppCommand(cmd, appConfigFile)
		case <-c.ctx.Done():
			c.stopAllAppGoroutines()
			return
		}
	}
}

func (c *Controller) clusterHealthChecker() {
	defer c.wg.Done()
	c.logger.Info("Cluster health checker started.")

	ticker := time.NewTicker(cluster.DefaultClusterHealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.clusters.RLock()
			clustersToCheck := c.clusters.List()
			c.clusters.RUnlock()
			for _, cl := range clustersToCheck {
				c.performClusterHealthCheck(c.ctx, cl)
			}
		case cmd, ok := <-c.clusterCommandChan:
			if !ok {
				return
			}
			if cmd.Type == ClusterCommandCheck {
				cl, exists := c.clusters.Get(cmd.ClusterName)
				if exists {
					c.performClusterHealthCheck(c.ctx, cl)
				}
			}
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Controller) performClusterHealthCheck(ctx context.Context, cl *cluster.Cluster) {
	logger := c.logger.With(zap.String("cluster", cl.Name))
	var status, message string
	k8sClient, err := k8s.NewClientSet(logger, cl.KubeconfigPath, cl.AllowedNamespaces)
	if err != nil {
		status = "Error"
		message = fmt.Sprintf("Failed to create K8s client: %v", err)
	} else {
		checkCtx, checkCancel := context.WithTimeout(ctx, K8sConnectTimeout)
		defer checkCancel()
		if err := k8sClient.CheckConnectivity(checkCtx); err != nil {
			status = "Unreachable"
			message = fmt.Sprintf("Connectivity failed: %v", err)
			metrics.ClusterStatus.WithLabelValues(cl.Name).Set(0)
		} else {
			status = "Active"
			message = "Connectivity successful."
			metrics.ClusterStatus.WithLabelValues(cl.Name).Set(1)
		}
	}

	c.clusters.Lock()
	cl.Status = status
	cl.Message = message
	cl.LastCheckedAt = time.Now()
	_ = cluster.SaveClusters(c.clusters, cluster.DefaultClusterConfigFile)
	c.clusters.Unlock()

	c.emit(ctx, events.TypeClusterHealthCompleted, map[string]any{
		"cluster": cl.Name,
		"status":  status,
		"message": message,
	})
}

func (c *Controller) handleAppCommand(cmd AppCommand, appConfigFile string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	switch cmd.Type {
	case AppCommandStart:
		c.apps.RLock()
		appConfig, exists := c.apps.Get(cmd.AppName)
		if !exists {
			c.apps.RUnlock()
			return
		}

		c.clusters.RLock()
		_, clusterExists := c.clusters.Get(appConfig.ClusterName)
		c.clusters.RUnlock()

		if !clusterExists && appConfig.ClusterName != "" {
			c.apps.RUnlock()
			appConfig.Status = "Error"
			appConfig.Message = fmt.Sprintf("Cluster '%s' does not exist", appConfig.ClusterName)
			c.saveAppStatus(appConfig, appConfigFile, true)
			return
		}

		if runtime, ok := c.runningApps[cmd.AppName]; ok {
			runtime.cancel()
		}

		appCopy := *appConfig
		c.apps.RUnlock()

		appCtx, appCancel := context.WithCancel(c.ctx)
		syncChan := make(chan struct{}, 1)

		c.wg.Add(1)
		c.runningApps[cmd.AppName] = &appRuntime{cancel: appCancel, syncChan: syncChan}
		go c.reconcileApp(appCtx, &appCopy, appConfigFile, appCancel, syncChan)

	case AppCommandStop:
		if runtime, ok := c.runningApps[cmd.AppName]; ok {
			runtime.cancel()
		}

	case AppCommandSync:
		if runtime, ok := c.runningApps[cmd.AppName]; ok {
			select {
			case runtime.syncChan <- struct{}{}:
			default:
			}
		}

	case AppCommandApprove:
		c.apps.Lock()
		appConfig, exists := c.apps.Get(cmd.AppName)
		if !exists {
			c.apps.Unlock()
			return
		}
		commitHash, _ := cmd.Data["commitHash"].(string)
		appConfig.ApprovedGitHash = commitHash
		_ = app.SaveApplications(c.apps, appConfigFile)
		c.apps.Unlock()
		c.TriggerSync(cmd.AppName)
	}
}

func (c *Controller) stopAllAppGoroutines() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, runtime := range c.runningApps {
		runtime.cancel()
	}
}

func (c *Controller) reconcileApp(appCtx context.Context, app *app.Application, appConfigFile string, appCancel context.CancelFunc, syncChan chan struct{}) {
	defer c.wg.Done()
	defer func() {
		c.mu.Lock()
		if rt, ok := c.runningApps[app.Name]; ok && rt.syncChan == syncChan {
			delete(c.runningApps, app.Name)
		}
		c.mu.Unlock()
		appCancel()
	}()

	logger := c.logger.With(zap.String("app", app.Name))

	if app.ClusterName == "" {
		app.Status = "Pending"
		app.Message = "Awaiting cluster assignment"
		c.saveAppStatus(app, appConfigFile, true)
	}

	repoDir, err := git.CreateTempRepoDir()
	if err != nil {
		app.Status = "Error"
		app.Message = fmt.Sprintf("Failed to create temp dir: %v", err)
		c.saveAppStatus(app, appConfigFile, true)
		return
	}
	defer func() { _ = git.CleanUpRepo(logger, repoDir) }()

	var client k8sApplier

	// Initialize client if cluster exists
	if app.ClusterName != "" {
		c.clusters.RLock()
		cl, ok := c.clusters.Get(app.ClusterName)
		c.clusters.RUnlock()
		if !ok {
			app.Status = "Error"
			app.Message = fmt.Sprintf("Cluster '%s' does not exist", app.ClusterName)
			c.saveAppStatus(app, appConfigFile, true)
			return
		}
		newClient, err := k8s.NewClientSet(c.logger, cl.KubeconfigPath, cl.AllowedNamespaces)
		if err != nil {
			app.Status = "Error"
			app.Message = fmt.Sprintf("Failed to create k8s client: %v", err)
			c.saveAppStatus(app, appConfigFile, true)
			return
		}
		client = newClient
	}

	interval := app.PollingInterval
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	c.performSync(appCtx, logger, app, repoDir, client, appConfigFile, "initial")
	if (app.Status == "Synced" || app.Status == "Healthy") && client != nil {
		c.checkHealth(appCtx, logger, app, client, appConfigFile)
	}

	// Terminal error on initial sync (e.g. cluster missing) — stop looping.
	if app.Status == "Error" {
		return
	}

	for {
		select {
		case <-ticker.C:
			c.performSync(appCtx, logger, app, repoDir, client, appConfigFile, "poll")
			c.checkHealth(appCtx, logger, app, client, appConfigFile)
		case <-syncChan:
			c.performSync(appCtx, logger, app, repoDir, client, appConfigFile, "manual")
			c.checkHealth(appCtx, logger, app, client, appConfigFile)

		case <-appCtx.Done():
			if app.Status != "Stopped" && app.Status != "Error" {
				app.Status = "Stopped"
				app.Message = "Controller shut down"
				c.saveAppStatus(app, appConfigFile, true)
			}
			return
		}
	}
}

// k8sApplier abstracts the Kubernetes apply operation so performSync can be
// unit-tested without a real cluster.
type k8sApplier interface {
	ApplyManifests(ctx context.Context, manifestsDir string, appName, clusterName string) ([]k8s.ResourceMetadata, []error)
	GetResourceHealth(ctx context.Context, r k8s.ResourceMetadata) (string, string, error)
	Prune(ctx context.Context, appName string, previous, current []k8s.ResourceMetadata, maxDeletions int) ([]k8s.ResourceMetadata, error)
}

func (c *Controller) performSync(ctx context.Context, logger *zap.Logger, application *app.Application, repoDir string, client k8sApplier, appConfigFile string, trigger string) {
	oldStatus := application.Status
	oldHash := application.LastSyncedGitHash
	c.emit(ctx, events.TypeAppSyncStarted, map[string]any{"app": application.Name, "trigger": trigger})

	currentHash, err := git.CloneOrPull(ctx, logger, application.RepoURL, application.Branch, repoDir)
	if err != nil {
		metrics.GitPullTotal.WithLabelValues(application.Name, "failure").Inc()
		application.Status = "Error"
		application.Message = fmt.Sprintf("Git error: %v", err)
		application.ConsecutiveFailures++
		c.saveAppStatus(application, appConfigFile, true)
		return
	}
	metrics.GitPullTotal.WithLabelValues(application.Name, "success").Inc()
	application.LatestGitHash = currentHash

	if currentHash == application.LastSyncedGitHash && trigger == "poll" {
		return
	}

	if application.SyncPolicy == "manual" && currentHash != application.ApprovedGitHash {
		application.Status = "OutOfSync"
		application.Message = fmt.Sprintf("New commit %s available (Manual sync required)", currentHash)
		c.saveAppStatus(application, appConfigFile, true)
		return
	}

	manifestsDir := filepath.Join(repoDir, application.Path)
	if _, err := os.Stat(manifestsDir); err != nil {
		application.Status = "Error"
		application.Message = fmt.Sprintf("Manifest path missing: %v", err)
		application.ConsecutiveFailures++
		c.saveAppStatus(application, appConfigFile, true)
		return
	}

	if client == nil {
		c.clusters.RLock()
		cl, ok := c.clusters.Get(application.ClusterName)
		c.clusters.RUnlock()
		if !ok {
			application.Status = "Error"
			application.Message = fmt.Sprintf("Cluster '%s' not found", application.ClusterName)
			c.saveAppStatus(application, appConfigFile, true)
			return
		}

		newClient, err := k8s.NewClientSet(c.logger, cl.KubeconfigPath, cl.AllowedNamespaces)
		if err != nil || newClient == nil {
			application.Status = "Error"
			application.Message = fmt.Sprintf("Failed to create k8s client for cluster '%s': %v", application.ClusterName, err)
			application.ConsecutiveFailures++
			c.saveAppStatus(application, appConfigFile, true)
			return
		}
		client = newClient
	}

	start := time.Now()
	applied, applyErrors := client.ApplyManifests(ctx, manifestsDir, application.Name, application.ClusterName)
	if len(applyErrors) > 0 {
		metrics.AppSyncTotal.WithLabelValues(application.Name, application.ClusterName, "failure").Inc()
		errMsg := fmt.Sprintf("Apply error: %v", applyErrors[0])
		application.Status = "Error"
		application.Message = errMsg
		application.ConsecutiveFailures++
	} else {
		metrics.AppSyncTotal.WithLabelValues(application.Name, application.ClusterName, "success").Inc()
		metrics.AppSyncDuration.WithLabelValues(application.Name, application.ClusterName).Observe(time.Since(start).Seconds())
		application.LastSyncedGitHash = currentHash
		application.Status = "Synced"
		application.Message = fmt.Sprintf("Synced to %s", currentHash)
		application.ConsecutiveFailures = 0

		previousResources := application.AppliedResources
		application.AppliedResources = toAppResources(applied)

		if application.Prune {
			retained, pruneErr := client.Prune(ctx, application.Name, toK8sResources(previousResources), applied, k8s.DefaultMaxPrune)
			application.AppliedResources = append(application.AppliedResources, toAppResources(retained)...)
			if pruneErr != nil {
				application.LastSyncedGitHash = oldHash
				application.Status = "Error"
				application.Message = fmt.Sprintf("Prune error: %v", pruneErr)
				application.ConsecutiveFailures++
			}
		}
	}

	c.notify(application, oldStatus, oldHash)
	c.saveAppStatus(application, appConfigFile, true)
}

func toAppResources(in []k8s.ResourceMetadata) []app.ResourceMetadata {
	out := make([]app.ResourceMetadata, 0, len(in))
	for _, r := range in {
		out = append(out, app.ResourceMetadata(r))
	}
	return out
}

func toK8sResources(in []app.ResourceMetadata) []k8s.ResourceMetadata {
	out := make([]k8s.ResourceMetadata, 0, len(in))
	for _, r := range in {
		out = append(out, k8s.ResourceMetadata(r))
	}
	return out
}

func (c *Controller) notify(application *app.Application, oldStatus, oldHash string) {
	if application.WebhookURL == "" {
		return
	}

	// Only notify if status changed OR a new commit was synced
	if application.Status == oldStatus && application.LastSyncedGitHash == oldHash {
		return
	}

	go notifications.SendWebhook(c.logger, application.WebhookURL, application.WebhookSecret, notifications.Notification{
		App:       application.Name,
		Cluster:   application.ClusterName,
		Status:    application.Status,
		Message:   application.Message,
		Commit:    application.LastSyncedGitHash,
		Timestamp: time.Now(),
	})
}

func (c *Controller) checkHealth(ctx context.Context, logger *zap.Logger, application *app.Application, client k8sApplier, appConfigFile string) {
	if client == nil || len(application.AppliedResources) == 0 {
		return
	}

	oldStatus := application.Status
	oldHash := application.LastSyncedGitHash

	overallStatus := "Healthy"
	var messages []string

	for _, r := range application.AppliedResources {
		status, msg, err := client.GetResourceHealth(ctx, k8s.ResourceMetadata{
			Group:     r.Group,
			Version:   r.Version,
			Kind:      r.Kind,
			Namespace: r.Namespace,
			Name:      r.Name,
		})

		if err != nil {
			logger.Warn("Failed to check health", zap.String("app", application.Name), zap.String("kind", r.Kind), zap.String("name", r.Name), zap.Error(err))
			continue
		}

		if status == "Degraded" {
			overallStatus = "Degraded"
			messages = append(messages, fmt.Sprintf("%s/%s: %s", r.Kind, r.Name, msg))
		} else if status == "Progressing" && overallStatus == "Healthy" {
			overallStatus = "Progressing"
			messages = append(messages, fmt.Sprintf("%s/%s: %s", r.Kind, r.Name, msg))
		}
	}

	if overallStatus != application.Status {
		application.Status = overallStatus
		if len(messages) > 0 {
			application.Message = strings.Join(messages, "; ")
		} else if overallStatus == "Healthy" {
			application.Message = "All resources healthy"
		}
		c.emit(ctx, events.TypeAppStatusChanged, map[string]any{"app": application.Name, "status": overallStatus})
		c.notify(application, oldStatus, oldHash)
		c.saveAppStatus(application, appConfigFile, true)
	}

	// Always report health metric
	val := 0.0
	switch overallStatus {
	case "Healthy":
		val = 1.0
	case "Progressing":
		val = 0.5
	}
	metrics.AppHealthStatus.WithLabelValues(application.Name, application.ClusterName).Set(val)
}

func (c *Controller) saveAppStatus(appToSave *app.Application, appConfigFile string, forceSave bool) {
	c.apps.Lock()
	defer c.apps.Unlock()

	originalApp, ok := c.apps.Apps[appToSave.Name]
	if !ok {
		return
	}

	originalApp.Status = appToSave.Status
	originalApp.Message = appToSave.Message
	originalApp.LastSyncedGitHash = appToSave.LastSyncedGitHash
	originalApp.ConsecutiveFailures = appToSave.ConsecutiveFailures
	originalApp.LatestGitHash = appToSave.LatestGitHash
	originalApp.AppliedResources = appToSave.AppliedResources

	_ = app.SaveApplications(c.apps, appConfigFile)
}

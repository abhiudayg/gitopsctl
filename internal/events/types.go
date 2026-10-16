package events

// Type identifies an event for integrations (dashboards, webhooks). Use stable strings.
type Type string

const (
	SpecVersion = "1.0"

	TypeControllerStarted           Type = "io.gitopsctl.controller.started"
	TypeControllerStopping          Type = "io.gitopsctl.controller.stopping"
	TypeAppRegistered               Type = "io.gitopsctl.app.registered"
	TypeAppUnregistered             Type = "io.gitopsctl.app.unregistered"
	TypeAppSyncRequested            Type = "io.gitopsctl.app.sync_requested"
	TypeAppSyncStarted              Type = "io.gitopsctl.app.sync_started"
	TypeAppGitPullFailed            Type = "io.gitopsctl.app.git_pull_failed"
	TypeAppManifestPathMissing      Type = "io.gitopsctl.app.manifest_path_missing"
	TypeAppApplyFailed              Type = "io.gitopsctl.app.apply_failed"
	TypeAppSyncSucceeded            Type = "io.gitopsctl.app.sync_succeeded"
	TypeAppSyncNoChanges            Type = "io.gitopsctl.app.sync_no_changes"
	TypeClusterRegistered           Type = "io.gitopsctl.cluster.registered"
	TypeClusterUnregistered         Type = "io.gitopsctl.cluster.unregistered"
	TypeClusterHealthCheckRequested Type = "io.gitopsctl.cluster.health_check_requested"
	TypeClusterHealthCompleted      Type = "io.gitopsctl.cluster.health_check_completed"
	TypeAppStatusChanged            Type = "io.gitopsctl.app.status_changed"
	TypeAPIAudit                    Type = "io.gitopsctl.api.audit"
)

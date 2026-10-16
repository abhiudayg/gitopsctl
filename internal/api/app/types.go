package app

import (
	appcore "aeswibon.com/github/gitopsctl/internal/core/app"
)

// RegisterRequest represents the request payload for registering an application.
// This structure is used in the API requests to register a new application with the GitOps controller.
type RegisterRequest struct {
	// Name is the unique identifier for the application.
	Name string `json:"name" validate:"required"`
	// RepoURL is the URL of the Git repository where the application's manifests are stored.
	RepoURL string `json:"repo_url" validate:"required,url"`
	// Branch is the branch in the Git repository that contains the application's manifests.
	Branch string `json:"branch" validate:"required"`
	// Path is the directory path within the repository where the manifests are located.
	Path string `json:"path" validate:"required"`
	// ClusterName is the name of the Kubernetes cluster where the application will be deployed.
	ClusterName string `json:"cluster_name" validate:"required"`
	// Cluster is a backwards-compatible alias for cluster_name.
	Cluster string `json:"cluster"`
	// Interval is the frequency at which the application should be synced with the Git repository.
	Interval string `json:"interval" validate:"required"`
}

// Response represents the response payload for application operations.
// This structure is used in the API responses to provide information about registered applications.
type Response struct {
	// Name is the unique identifier for the application.
	Name string `json:"name"`
	// RepoURL is the URL of the Git repository where the application's manifests are stored.
	RepoURL string `json:"repo_url"`
	// Branch is the branch in the Git repository that contains the application's manifests.
	Branch string `json:"branch"`
	// Path is the directory path within the repository where the manifests are located.
	Path string `json:"path"`
	// ClusterName is the name of the Kubernetes cluster where the application will be deployed.
	ClusterName string `json:"cluster_name"`
	// Interval is the frequency at which the application should be synced with the Git repository.
	Interval string `json:"interval"`
	// LastSyncedGitHash is the last commit hash that was successfully synced from the Git repository.
	LastSyncedGitHash string `json:"last_synced_git_hash"`
	// LatestGitHash is the most recent commit hash discovered by the controller.
	LatestGitHash string `json:"latest_git_hash"`
	// Status indicates the current status of the application (e.g., "active", "inactive", "error").
	Status string `json:"status"`
	// Message provides additional information about the application's status, such as error messages or warnings.
	Message string `json:"message"`
	// ConsecutiveFailures counts the number of consecutive sync failures for the application.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// SyncPolicy determines how changes are applied ("auto" or "manual").
	SyncPolicy string `json:"sync_policy"`
	// Prune reports whether resources removed from Git are deleted from the cluster.
	Prune bool `json:"prune"`
	// ApprovedGitHash is the commit hash currently approved for deployment in manual sync mode.
	ApprovedGitHash string `json:"approved_git_hash"`
	// LastUpdated is the timestamp of the last update to the application's status.
	LastUpdated string `json:"last_updated"`
}

// SyncTriggerResponse represents the response for sync trigger requests.
type SyncTriggerResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ConvertToResponse converts an Application to a Response.
func ConvertToResponse(app *appcore.Application) Response {
	return Response{
		Name:                app.Name,
		RepoURL:             app.RepoURL,
		Branch:              app.Branch,
		Path:                app.Path,
		ClusterName:         app.ClusterName,
		Interval:            app.Interval,
		LastSyncedGitHash:   app.LastSyncedGitHash,
		LatestGitHash:       app.LatestGitHash,
		Status:              app.Status,
		Message:             app.Message,
		ConsecutiveFailures: app.ConsecutiveFailures,
		SyncPolicy:          app.SyncPolicy,
		Prune:               app.Prune,
		ApprovedGitHash:     app.ApprovedGitHash,
	}
}

// PlanItemResponse describes one resource in a plan.
type PlanItemResponse struct {
	Action    string `json:"action"`
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// PlanResponse is returned by the plan endpoint.
type PlanResponse struct {
	App    string             `json:"app"`
	Commit string             `json:"commit"`
	Items  []PlanItemResponse `json:"items"`
}

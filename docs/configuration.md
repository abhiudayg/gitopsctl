# Configuration

GitOpsCTL stores runtime state and user configuration in JSON files under `configs/` by default.

| File | Purpose |
|------|---------|
| `configs/applications.json` | Registered applications, sync policy, status, Git metadata, and applied resources. |
| `configs/clusters.json` | Registered Kubernetes clusters, kubeconfig paths, status, and namespace restrictions. |

Older examples may refer to `apps.json`. The current default application config file is `configs/applications.json`.

## Application Configuration

Applications are stored as a JSON array in `configs/applications.json`.

```json
[
  {
    "name": "nginx-demo",
    "repoURL": "https://github.com/aeswibon/gitopsctl.git",
    "branch": "main",
    "path": "examples/manifests",
    "clusterName": "local-dev",
    "interval": "30s",
    "syncPolicy": "auto",
    "status": "Pending",
    "message": "Registered, awaiting first sync"
  }
]
```

### Application Fields

| Field | Required | Written by | Description |
|-------|----------|------------|-------------|
| `name` | Yes | User | Unique application name. Use DNS-friendly names such as `frontend-prod`. |
| `repoURL` | Yes | User | Git repository URL. HTTPS and SSH Git URLs are supported. |
| `branch` | No | User | Branch to watch. Defaults to `main` when registering through the CLI. |
| `path` | Yes | User | Directory inside the repo containing raw YAML, a Kustomize overlay, or a Helm chart. |
| `clusterName` | Yes | User | Name of a cluster from `configs/clusters.json`. |
| `interval` | No | User | Polling interval such as `30s`, `5m`, or `1h`. CLI validation allows 10 seconds through 24 hours. |
| `syncPolicy` | No | User | `auto` applies discovered commits. `manual` records the latest commit and waits for approval. |
| `approvedGitHash` | No | User/API | Commit approved for a manual sync. Usually set through `gitopsctl approve-app`. |
| `latestGitHash` | No | Controller | Latest commit hash discovered by the controller. |
| `lastSyncedGitHash` | No | Controller | Last commit hash successfully applied. |
| `status` | No | Controller | Current app status such as `Pending`, `Synced`, `Healthy`, `Progressing`, `Degraded`, `OutOfSync`, `Error`, or `Stopped`. |
| `message` | No | Controller | Human-readable status or error detail. |
| `consecutiveFailures` | No | Controller | Count of consecutive sync failures. |
| `webhookUrl` | No | User | Per-application sync notification webhook URL. |
| `webhookSecret` | No | User | Optional HMAC signing secret for per-application webhook notifications. |
| `appliedResources` | No | Controller | Kubernetes resources applied by the most recent sync, used for app health checks. |

Use `interval`, not `pollingInterval`, in JSON. `pollingInterval` is an internal parsed duration and is not read from config.

## Cluster Configuration

Clusters are stored as a JSON array in `configs/clusters.json`.

```json
[
  {
    "name": "local-dev",
    "kubeconfigPath": "/Users/you/.kube/config",
    "registeredAt": "2026-05-10T10:00:00Z",
    "status": "Pending",
    "message": "Cluster registered, awaiting validation",
    "allowedNamespaces": ["demo"]
  }
]
```

### Cluster Fields

| Field | Required | Written by | Description |
|-------|----------|------------|-------------|
| `name` | Yes | User | Unique cluster name referenced by applications. |
| `kubeconfigPath` | Yes | User | Absolute path to the kubeconfig file used by the controller. |
| `registeredAt` | No | CLI | Registration timestamp. |
| `status` | No | Controller | Cluster status such as `Pending`, `Active`, `Unreachable`, or `Error`. |
| `message` | No | Controller | Human-readable cluster status or error detail. |
| `lastCheckedAt` | No | Controller | Last cluster health check timestamp. |
| `allowedNamespaces` | No | User | Optional list of namespaces the controller may write to for this cluster. Empty means unrestricted. |

When `allowedNamespaces` is set, namespaced manifests outside the list are rejected before apply.

## Registering Through the CLI

Prefer CLI registration for day-to-day use because it validates names, paths, URLs, intervals, and kubeconfig locations.

```bash
gitopsctl register-cluster \
  --name local-dev \
  --kubeconfig ~/.kube/config \
  --allowed-namespaces demo

gitopsctl register-apps \
  --name nginx-demo \
  --repo https://github.com/aeswibon/gitopsctl.git \
  --branch main \
  --path examples/manifests \
  --cluster local-dev \
  --interval 30s \
  --sync-policy auto
```

## Manual Sync Policy

Manual sync keeps Git discovery separate from cluster apply.

1. The controller polls Git and records the latest commit hash.
2. If the latest hash is not approved, the app becomes `OutOfSync`.
3. A user approves a commit through the API or CLI.
4. The controller applies only the approved commit.

```bash
gitopsctl approve-app --name nginx-demo --commit <commit-hash>
```

## Manifest Directory Detection

For each application `path`, GitOpsCTL applies one of these modes:

| Directory contents | Behavior |
|--------------------|----------|
| `Chart.yaml` or `Chart.yml` | Render as a Helm chart in client-only dry-run mode, then apply rendered YAML. |
| `kustomization.yaml`, `kustomization.yml`, or `Kustomization` | Build with Kustomize, then apply generated YAML. |
| Other `.yaml` or `.yml` files | Apply raw YAML files recursively. |

SOPS-encrypted `.yaml`, `.yml`, and `.json` files are decrypted before render/apply when the controller environment has the required keys.

## API and Event Flags

Global flags apply before the subcommand:

```bash
gitopsctl --api-url http://127.0.0.1:8080 sync-app --name nginx-demo
```

Controller flags apply to `start`:

```bash
gitopsctl start \
  --api-address :8080 \
  --events-file configs/events.jsonl \
  --events-webhook https://example.com/gitopsctl/events \
  --events-webhook-secret "$WEBHOOK_SECRET"
```

| Flag | Command | Default | Description |
|------|---------|---------|-------------|
| `--api-url` | global | `http://127.0.0.1:8080` | Base URL used by API-backed commands and dashboard. |
| `--api-address`, `-a` | `start` | `:8080` | Listen address for REST API, SSE, and metrics. |
| `--api-token` | global | `$GITOPSCTL_API_TOKEN` | Bearer token. With `start` it is accepted as an admin token; with other commands it is sent to the API. |
| `--api-token-file` | `start` | empty | File with one `<scope> <token>` per line (`read`, `sync`, `admin`). |
| `--api-cors-origin` | `start` | none | Browser origin allowed to call the API (repeatable). CORS is disabled by default. |
| `--events-file` | global/start | empty | Append integration events as JSON lines. |
| `--events-webhook` | global/start | empty | POST integration events to an HTTP endpoint. |
| `--events-webhook-bearer` | global/start | empty | Bearer token for event webhook requests. |
| `--events-webhook-secret` | global/start | empty | HMAC signing secret for event webhook payloads. |
| `--events-webhook-retries` | global/start | `2` | Retry attempts for transient webhook failures. |
| `--events-webhook-backoff` | global/start | `750ms` | Base retry backoff. |
| `--events-webhook-timeout` | global/start | `12s` | HTTP timeout per webhook attempt. |

## Configuration Hygiene

- Use absolute kubeconfig paths in committed or copied cluster configs.
- Do not commit kubeconfigs, tokens, decrypted secrets, or webhook secrets.
- Keep `allowedNamespaces` narrow for shared clusters.
- Keep examples and documentation in sync with CLI command names and JSON field names.

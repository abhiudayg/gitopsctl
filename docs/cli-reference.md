# CLI Reference

This page summarizes the user-facing GitOpsCTL commands. Run `gitopsctl <command> --help` for the exact help text from your build.

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--api-url` | `http://127.0.0.1:8080` | Base URL used by commands that talk to a running controller API. |
| `--events-file` | empty | Append command/controller integration events as JSON lines. |
| `--events-webhook` | empty | POST integration events to an HTTP endpoint. |
| `--events-webhook-bearer` | empty | Bearer token for webhook requests. |
| `--events-webhook-secret` | empty | HMAC signing secret for webhook requests. |
| `--events-webhook-retries` | `2` | Retry attempts for webhook events. |
| `--events-webhook-backoff` | `750ms` | Base retry backoff. |
| `--events-webhook-timeout` | `12s` | HTTP timeout per webhook attempt. |

Global flags come before or after the subcommand depending on Cobra parsing, but placing them before the command is the least surprising form:

```bash
gitopsctl --api-url http://127.0.0.1:8080 sync-app --name nginx-demo
```

## Controller

### `start`

Starts the controller and API server.

```bash
gitopsctl start --api-address :8080
```

Flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--api-address`, `-a` | `:8080` | Listen address for REST API, SSE, and metrics. Without an API token the server binds to loopback only. |
| `--api-token` | `$GITOPSCTL_API_TOKEN` | Admin bearer token required on `/api/v1`. `/health` and `/metrics` stay open. |
| `--api-token-file` | empty | File with one `<scope> <token>` per line. `read` allows GET, `sync` adds `sync-app`/`approve-app`/`check-cluster`, `admin` allows everything. |
| `--api-cors-origin` | none | Allowed browser origin (repeatable). CORS is off by default. |

Mutating API calls emit an `io.gitopsctl.api.audit` event with a non-secret token id, method, path, and status. API-backed commands and the dashboard send `--api-token` (or `$GITOPSCTL_API_TOKEN`) automatically.

## Applications

### `register-apps`

Registers an application in `configs/applications.json`.

```bash
gitopsctl register-apps \
  --name nginx-demo \
  --repo https://github.com/aeswibon/gitopsctl.git \
  --branch main \
  --path examples/manifests \
  --cluster local-dev \
  --interval 30s \
  --sync-policy auto
```

Important flags:

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n` | Yes | Application name. |
| `--repo` | `-r` | Yes | Git repository URL. |
| `--path` | `-p` | Yes | Manifest path inside the repo. |
| `--cluster` | `-c` | Yes | Registered cluster name. |
| `--branch` | `-b` | No | Git branch, default `main`. |
| `--interval` | `-i` | No | Polling interval, default `5m`. |
| `--sync-policy` | | No | `auto` or `manual`, default `auto`. |
| `--prune` | | No | Delete resources owned by this app (labelled `gitopsctl.io/app`) that were removed from Git. Off by default; protected kinds (Namespace, CRD, PV, PVC) are never deleted and a sync that would delete more than 10 resources is refused. |
| `--webhook-url` | | No | Per-app notification webhook. |
| `--webhook-secret` | | No | Per-app webhook signing secret. |
| `--dry-run` | | No | Preview without saving. |
| `--force` | | No | Overwrite an existing app entry. |

### `list-apps`

Lists registered applications.

```bash
gitopsctl list-apps
```

### `status-apps`

Shows application status and sync metadata.

```bash
gitopsctl status-apps
```

### `sync-app`

Requests immediate reconciliation through the running API server.

```bash
gitopsctl --api-url http://127.0.0.1:8080 sync-app --name nginx-demo
```

### `diff-app`

Shows what a sync would create, update or prune without applying anything. Requires a running controller; needs the `sync` scope when API tokens are scoped.

```bash
gitopsctl diff-app --name nginx-demo
```

Prune candidates appear only for applications registered with `--prune`.

### `approve-app`

Approves a commit for a manual-sync application.

```bash
gitopsctl --api-url http://127.0.0.1:8080 approve-app \
  --name nginx-demo \
  --commit <commit-hash>
```

### `unregister`

Removes an application registration.

```bash
gitopsctl unregister --name nginx-demo
```

## Clusters

### `register-cluster`

Registers a Kubernetes cluster in `configs/clusters.json`.

```bash
gitopsctl register-cluster \
  --name local-dev \
  --kubeconfig ~/.kube/config \
  --allowed-namespaces demo
```

Important flags:

| Flag | Short | Required | Description |
|------|-------|----------|-------------|
| `--name` | `-n` | Yes | Cluster name. |
| `--kubeconfig` | `-k` | No | Kubeconfig path. Auto-detected from `$KUBECONFIG` or `~/.kube/config` when omitted. |
| `--allowed-namespaces` | | No | Comma-separated namespace allow-list. |
| `--test` | | No | Validate kubeconfig loading during registration. |
| `--dry-run` | | No | Preview without saving. |
| `--force` | | No | Overwrite an existing cluster entry. |

### `list-clusters`

Lists registered clusters.

```bash
gitopsctl list-clusters
```

### `status-clusters`

Shows cluster connectivity status.

```bash
gitopsctl status-clusters
```

### `check-cluster`

Requests a health check through the running API server.

```bash
gitopsctl --api-url http://127.0.0.1:8080 check-cluster --name local-dev
```

### `unregister-cluster`

Removes a cluster registration.

```bash
gitopsctl unregister-cluster --name local-dev
```

## Dashboard and Events

### `dashboard`

Opens the terminal dashboard.

```bash
gitopsctl dashboard --api-url http://127.0.0.1:8080
```

### `tail-events`

Reads a JSONL event file.

```bash
gitopsctl tail-events --file configs/events.jsonl --from-start
```

Useful flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--file` | `configs/events.jsonl` | Event file path. |
| `--follow` | `true` | Continue reading appended events. |
| `--from-start` | `false` | Print existing lines first. |
| `--poll-interval` | `400ms` | File polling interval. |

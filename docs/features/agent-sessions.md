# Agent Sessions

Agent sessions let a coding agent deploy and test a change in an isolated namespace without holding cluster credentials. The agent only has an API token; the controller keeps the kubeconfig.

## How it works

1. The agent calls `POST /api/v1/sessions`. The controller creates a namespace `agent-<agent>-<session>` with a quota, labelled `gitopsctl.io/session`, and expiry time.
2. The agent calls `POST /api/v1/sessions/:id/deploy` with a repository, branch and path. The controller clones the repository, renders it (plain YAML, Kustomize or Helm), and checks every rendered object against the policy before anything is applied. One violation rejects the whole set.
3. The agent calls `DELETE /api/v1/sessions/:id` when done. Sessions that are not deleted are removed when they expire. A background janitor retries interrupted teardowns, removes half-created sessions after a restart, and deletes labelled namespaces that have no session record.

A namespace is only deleted when it carries both `app.kubernetes.io/managed-by=gitopsctl` and the matching session label.

## Agents file

```yaml
policy:
  allowed_kinds: [ConfigMap, Service, apps/Deployment]
  allowed_namespaces: ["agent-*"]
  denied_namespaces: []
  allowed_registries: [ghcr.io/acme, docker.io]
  max_objects: 50
  max_replicas: 3
agents:
  - id: claude
    cluster: dev
    repos: ["https://github.com/acme/"]
    max_sessions: 3
    default_ttl: 30m
    max_ttl: 2h
    quota: {pods: "10", limits.cpu: "2", limits.memory: 4Gi}
```

The file is parsed strictly and the controller refuses to start on any unknown field or invalid value.

- `repos`: an entry ending in `/` is a prefix; any other entry must match exactly.
- `allowed_kinds`: `Kind` for core kinds, `group/Kind` otherwise. Namespace, CRD, RBAC, webhook configuration and PersistentVolume kinds can never be allowed.
- Every agent's session namespaces must be permitted by `allowed_namespaces`.

## Policy rules

Cluster-scoped objects, kinds outside `allowed_kinds`, namespaces outside the session, labels under `gitopsctl.io/`, privileged containers, `allowPrivilegeEscalation: true`, added capabilities, host namespaces, `hostPath` volumes, `hostPort`, containers that can run as root, missing cpu/memory limits, images outside `allowed_registries`, and counts above `max_objects` / `max_replicas`. Rules cover Pod, Deployment, StatefulSet, DaemonSet, ReplicaSet, Job and CronJob, including init and ephemeral containers.

## Tokens

Add agent tokens to the `--api-token-file`:

```text
admin  <admin-token>
agent  <token-for-claude>  claude
```

An agent token can use only `/api/v1/sessions` and only sees its own sessions. Admin tokens can manage every session and must name the agent when creating one. Agent calls are audited with the agent id.

## API

| Method and path | Description |
|-----------------|-------------|
| `POST /api/v1/sessions` | Body `{"ttl":"30m"}` (admins add `"agent"`). Returns 201 with the session. |
| `GET /api/v1/sessions`, `GET /api/v1/sessions/:id` | Own sessions (all for admins). |
| `POST /api/v1/sessions/:id/deploy` | Body `{"repo":"...","ref":"branch","path":"dir"}`. 422 with a `violations` list when the policy rejects the manifests. |
| `DELETE /api/v1/sessions/:id` | 200 when removed, 202 when teardown is still pending and will be retried. |

Status codes: 403 repository not allowed, 400 invalid path or ref, 404 unknown session (also for other agents' sessions), 409 session expired or tearing down, 429 session limit reached.

## Limits of v1

The controller still uses one kubeconfig for all sessions, so isolation relies on the policy and namespaces rather than Kubernetes RBAC. Running the controller in the cluster with per-session RBAC is planned.

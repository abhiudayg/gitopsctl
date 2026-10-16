# GitOpsCTL

<p align="center">
  <img src="assets/logo.png" alt="GitOpsCTL Logo" width="200" />
</p>

[![Build Status](https://github.com/aeswibon/gitopsctl/actions/workflows/ci.yml/badge.svg)](https://github.com/aeswibon/gitopsctl/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/aeswibon/gitopsctl)](https://goreportcard.com/report/github.com/aeswibon/gitopsctl)

GitOpsCTL is a lightweight, external GitOps control plane for Kubernetes. It watches Git repositories, renders plain YAML, Kustomize, or Helm manifests, applies them to registered clusters, and exposes a CLI, REST API, Server-Sent Events stream, Prometheus metrics, JSONL event log, webhooks, and an interactive terminal dashboard.

Unlike in-cluster GitOps controllers, GitOpsCTL can run from a laptop, CI runner, bastion host, management VM, or container while managing one or more remote Kubernetes clusters through kubeconfig files.

## What It Does

- Watches application Git repositories on a configurable interval.
- Applies raw Kubernetes YAML, Kustomize overlays, or Helm charts.
- Supports automatic sync and manual approval workflows.
- Manages multiple clusters from one controller process.
- Restricts cluster writes to configured namespaces when `allowedNamespaces` is set.
- Decrypts SOPS-encrypted YAML, YML, and JSON manifests before apply.
- Tracks app and cluster status in local JSON config files.
- Exposes an API and TUI for operational commands and live status.
- Emits integration events to SSE, JSONL files, and HTTP webhooks.
- Publishes Prometheus metrics for syncs, cluster health, app health, Git pulls, and Kubernetes applies.

## Quick Start

Prerequisites:

- A Kubernetes cluster reachable through `kubectl`.
- A kubeconfig file for that cluster.
- GitOpsCTL installed. See [Installation](docs/installation.md).

```bash
# 1. Register a cluster.
gitopsctl register-cluster \
  --name local-dev \
  --kubeconfig ~/.kube/config \
  --allowed-namespaces demo

# 2. Register the example nginx app.
gitopsctl register-apps \
  --name nginx-demo \
  --repo https://github.com/aeswibon/gitopsctl.git \
  --branch main \
  --path examples/manifests \
  --cluster local-dev \
  --interval 30s \
  --sync-policy auto

# 3. Start the controller and API server.
gitopsctl start --api-address :8080

# 4. In another terminal, open the dashboard.
gitopsctl dashboard --api-url http://127.0.0.1:8080
```

To start from checked-in sample config files instead of registering resources manually, see [Examples](examples/README.md).

## Documentation

- [Getting Started](docs/getting-started.md): First local sync, dashboard, status checks, and cleanup.
- [Installation](docs/installation.md): Install from releases, Go, Docker, or source.
- [Configuration](docs/configuration.md): Complete `applications.json` and `clusters.json` reference.
- [CLI Reference](docs/cli-reference.md): Commands, flags, and common workflows.
- [Architecture](docs/architecture.md): Controller, API, event bus, reconciliation, and storage model.
- [Terminal Dashboard](docs/features/tui.md): TUI views and keyboard controls.
- [Security](docs/features/security.md): Kubeconfig hygiene, namespace restrictions, RBAC, and SOPS.
- [SOPS](docs/SOPS.md): Secret encryption and decryption setup.
- [Observability](docs/features/observability.md): Metrics, events, JSONL audit logs, webhooks, and SSE.
- [Troubleshooting](docs/troubleshooting.md): Common setup and runtime failures.

## Repository Layout

```text
cmd/                    Cobra CLI commands
internal/api/           REST API, SSE stream, metrics endpoint
internal/controller/    Reconciliation loop and command dispatch
internal/core/app/      Application model and persistence
internal/core/cluster/  Cluster model and persistence
internal/core/git/      Git clone, pull, and commit helpers
internal/core/k8s/      Kubernetes client, render, apply, health logic
internal/events/        Event bus, history, stream, file, webhook sinks
internal/tui/           Bubble Tea terminal dashboard
docs/                   User and architecture documentation
examples/               Runnable sample configs and manifests
configs/                Default local runtime config directory
```

## Development

```bash
go test ./...
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

The project expects tests for new behavior and keeps coverage high across core packages.

## Contributing

Contributions are welcome. Please read [CONTRIBUTING.md](CONTRIBUTING.md), run the test suite, and keep docs updated when changing commands, flags, config fields, or user-visible behavior.

## License

GitOpsCTL is licensed under the Apache License 2.0. See [LICENSE](LICENSE).

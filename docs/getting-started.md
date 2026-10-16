# Getting Started

This guide walks through a local GitOpsCTL setup using the example nginx manifests in this repository.

## Prerequisites

- A Kubernetes cluster such as Kind, Minikube, OrbStack, Docker Desktop, or a remote dev cluster.
- `kubectl` configured for that cluster.
- GitOpsCTL installed. See [Installation](installation.md).
- Network access from the GitOpsCTL process to the Git repository and Kubernetes API server.

Confirm Kubernetes access:

```bash
kubectl cluster-info
kubectl get namespace default
```

## Option 1: Register Resources With Commands

This path is best for learning the CLI.

### 1. Register a Cluster

```bash
gitopsctl register-cluster \
  --name local-dev \
  --kubeconfig ~/.kube/config \
  --allowed-namespaces demo
```

Useful variants:

```bash
# Auto-detect kubeconfig from $KUBECONFIG or ~/.kube/config.
gitopsctl register-cluster --name local-dev

# Validate kubeconfig loading during registration.
gitopsctl register-cluster --name local-dev --kubeconfig ~/.kube/config --test

# Preview without writing configs/clusters.json.
gitopsctl register-cluster --name local-dev --kubeconfig ~/.kube/config --dry-run
```

### 2. Register an Application

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

This writes the application entry to `configs/applications.json`. The controller later clones the repo, enters `examples/manifests`, decrypts SOPS files when needed, renders the manifests, and applies them to the `local-dev` cluster.

### 3. Start the Controller

```bash
gitopsctl start --api-address :8080
```

The controller loads:

- `configs/applications.json`
- `configs/clusters.json`

It then starts reconciliation goroutines, the REST API, the SSE event stream, and the Prometheus metrics endpoint.

### 4. Watch Status

In another terminal:

```bash
gitopsctl status-apps
gitopsctl status-clusters
gitopsctl dashboard --api-url http://127.0.0.1:8080
```

You can also inspect Kubernetes directly:

```bash
kubectl get namespace demo
kubectl get deployment,service -n demo
```

### 5. Trigger a Manual Sync

For automatic apps, this requests an immediate reconciliation:

```bash
gitopsctl --api-url http://127.0.0.1:8080 sync-app --name nginx-demo
```

For manual apps, approve the commit hash shown in app status:

```bash
gitopsctl --api-url http://127.0.0.1:8080 approve-app \
  --name nginx-demo \
  --commit <commit-hash>
```

### 6. Clean Up

```bash
kubectl delete namespace demo
gitopsctl unregister --name nginx-demo
gitopsctl unregister-cluster --name local-dev
```

## Option 2: Use Example Config Files

This path is fastest when working from a local checkout.

```bash
mkdir -p configs
cp examples/configs/apps.json configs/applications.json
cp examples/configs/clusters.json configs/clusters.json
```

Edit `configs/clusters.json` and set `kubeconfig_path` to the absolute path of your kubeconfig.

Then run:

```bash
gitopsctl start --api-address :8080
```

## What Success Looks Like

- `gitopsctl status-clusters` shows the cluster as `Active` or recently checked.
- `gitopsctl status-apps` shows `Synced` after the first successful apply.
- `kubectl get all -n demo` shows the nginx deployment and service.
- `gitopsctl dashboard` lists the app and cluster without connection errors.

## Next Steps

- Read [Configuration](configuration.md) for every supported field.
- Read [CLI Reference](cli-reference.md) for command workflows.
- Enable [Observability](features/observability.md) with `--events-file`, webhooks, and metrics.
- Review [Security](features/security.md) before running against shared clusters.

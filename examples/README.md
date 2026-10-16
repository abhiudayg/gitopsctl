# GitOpsCTL Examples

This directory contains a runnable nginx demo for GitOpsCTL.

## Contents

```text
examples/
  configs/
    apps.json         Sample application config. Copy to configs/applications.json.
    clusters.json     Sample cluster config. Copy to configs/clusters.json and edit kubeconfig_path.
  manifests/
    namespace.yaml
    nginx-deployment.yaml
    nginx-service.yaml
    secret.sops.yaml.example
```

## Run the Demo From a Local Checkout

```bash
mkdir -p configs
cp examples/configs/apps.json configs/applications.json
cp examples/configs/clusters.json configs/clusters.json
```

Edit `configs/clusters.json`:

- Set `kubeconfig_path` to your real kubeconfig path.
- Keep `allowed_namespaces` as `["demo"]` unless you change the manifests.

Start GitOpsCTL:

```bash
gitopsctl start --api-address :8080
```

In another terminal:

```bash
gitopsctl status-apps
gitopsctl status-clusters
gitopsctl dashboard --api-url http://127.0.0.1:8080
```

Validate the Kubernetes resources:

```bash
kubectl get namespace demo
kubectl get deployment,service -n demo
```

## Register the Same Demo With Commands

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

## SOPS Example

`examples/manifests/secret.sops.yaml.example` is a template showing where encrypted SOPS metadata belongs. Do not apply it directly. To test SOPS:

1. Create a normal Kubernetes Secret manifest.
2. Encrypt it with `sops`.
3. Commit the encrypted file as `.yaml`, `.yml`, or `.json`.
4. Run GitOpsCTL in an environment that has access to the matching Age, PGP, or KMS key.

See [SOPS Secret Management](../docs/SOPS.md).

## Cleanup

```bash
kubectl delete namespace demo
gitopsctl unregister --name nginx-demo
gitopsctl unregister-cluster --name local-dev
```

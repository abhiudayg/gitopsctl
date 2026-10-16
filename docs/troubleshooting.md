# Troubleshooting

This guide covers common GitOpsCTL setup and runtime issues.

## Controller Starts With No Apps or Clusters

Symptoms:

- `gitopsctl start` logs that no applications or clusters are registered.

Checks:

```bash
ls -l configs/
cat configs/applications.json
cat configs/clusters.json
```

Fix:

- Register resources with `gitopsctl register-cluster` and `gitopsctl register-apps`.
- Or copy examples:

```bash
mkdir -p configs
cp examples/configs/apps.json configs/applications.json
cp examples/configs/clusters.json configs/clusters.json
```

## Application Config Fails to Load

Symptoms:

- Error mentions invalid polling interval.
- App never appears after copying example JSON.

Fix:

- Use the JSON field `interval`, not `pollingInterval`.
- Use values accepted by Go durations: `30s`, `5m`, `1h`.
- Keep interval between 10 seconds and 24 hours when using the CLI.

## Cluster Cannot Connect

Symptoms:

- Cluster status is `Unreachable` or `Error`.
- Syncs fail before applying manifests.

Checks:

```bash
kubectl --kubeconfig /path/from/config cluster-info
kubectl --kubeconfig /path/from/config auth can-i get namespaces
```

Fix:

- Make `kubeconfig_path` absolute.
- If running in Docker, use the container path, not the host path.
- Confirm the API server is reachable from the GitOpsCTL runtime.
- Confirm the kubeconfig user or service account has required RBAC.

## Namespace Is Not Allowed

Symptoms:

- Sync fails with a namespace allow-list error.

Fix:

- Add the manifest namespace to the cluster `allowed_namespaces`.
- Or change the manifest namespace.
- Or remove `allowed_namespaces` for unrestricted operation.

Example:

```json
{
  "name": "local-dev",
  "kubeconfig_path": "/Users/you/.kube/config",
  "allowed_namespaces": ["demo", "default"]
}
```

## Kubernetes Namespace Does Not Exist

Symptoms:

- Applying a Deployment, Service, or Secret fails because the namespace is missing.

Fix:

- Add a Namespace manifest to the app path.
- Or create the namespace separately:

```bash
kubectl create namespace demo
```

## Git Clone or Pull Fails

Symptoms:

- App status is `Error`.
- Message starts with `Git error`.

Fix:

- Confirm the repo URL is valid.
- Confirm the branch exists.
- For private repos, make SSH keys or tokens available to the GitOpsCTL process.
- Confirm outbound network access from the runtime.

## Manual App Is OutOfSync

Symptoms:

- App status is `OutOfSync`.
- Message references latest and approved commit hashes.

Fix:

```bash
gitopsctl status-apps
gitopsctl approve-app --name <app> --commit <latest-hash>
```

Then trigger a sync if needed:

```bash
gitopsctl sync-app --name <app>
```

## Dashboard Cannot Connect

Symptoms:

- Dashboard opens with errors or empty data.

Fix:

```bash
curl http://127.0.0.1:8080/metrics
curl http://127.0.0.1:8080/health
gitopsctl dashboard --api-url http://127.0.0.1:8080
```

- Confirm `gitopsctl start` is running.
- Confirm port mapping when using Docker.
- Match `dashboard --api-url` to `start --api-address`.

## SOPS Decryption Fails

Symptoms:

- Sync fails while walking or decrypting manifest files.

Fix:

- Confirm the encrypted file is valid with `sops -d file.sops.yaml`.
- Confirm Age, PGP, or cloud KMS credentials are available to the controller.
- Do not commit plaintext replacement files.
- See [SOPS Secret Management](SOPS.md).

## Event File Is Empty

Symptoms:

- `tail-events` shows no output.

Fix:

- Start the controller with `--events-file`.
- Trigger a sync or cluster check.

```bash
gitopsctl start --events-file configs/events.jsonl
gitopsctl tail-events --file configs/events.jsonl --from-start
```

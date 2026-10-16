# SOPS Secret Management

GitOpsCTL can decrypt SOPS-encrypted manifests before applying them to Kubernetes. This lets you keep encrypted Secrets in Git while applying plaintext only inside the controller's temporary working directory.

## How Decryption Works

During each application sync:

1. GitOpsCTL clones or pulls the application repository into a temporary directory.
2. It walks the configured manifest path.
3. It attempts SOPS decryption for `.yaml`, `.yml`, and `.json` files.
4. Files that are encrypted are written back decrypted inside the temporary checkout.
5. The manifest engine renders Helm, Kustomize, or raw YAML.
6. Kubernetes resources are applied.
7. The temporary checkout is removed after reconciliation.

Unencrypted files are left unchanged.

## Supported Providers

GitOpsCTL uses the SOPS library, so it can use the providers supported by SOPS when the controller environment is configured correctly:

- Age
- PGP
- AWS KMS
- GCP KMS
- Azure Key Vault
- HashiCorp Vault, when configured through SOPS

## Age Example

Create a key:

```bash
age-keygen -o age.key
export SOPS_AGE_KEY_FILE="$PWD/age.key"
```

Create `.sops.yaml`:

```yaml
creation_rules:
  - path_regex: .*\.sops\.ya?ml$
    age: age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

Encrypt a Kubernetes Secret:

```bash
kubectl create secret generic demo-secret \
  --namespace demo \
  --from-literal=password=change-me \
  --dry-run=client \
  -o yaml > secret.yaml

sops --encrypt secret.yaml > secret.sops.yaml
rm secret.yaml
```

Commit `secret.sops.yaml` and `.sops.yaml`. Do not commit `age.key`.

Run GitOpsCTL with access to the key:

```bash
export SOPS_AGE_KEY_FILE=/secure/path/age.key
gitopsctl start
```

## PGP Example

```bash
sops --encrypt --pgp <PGP_FINGERPRINT> secret.yaml > secret.sops.yaml
```

The controller host must have the matching private key available to GPG.

## Cloud KMS Examples

AWS:

```bash
sops --encrypt --kms arn:aws:kms:us-east-1:123456789012:key/<key-id> secret.yaml > secret.sops.yaml
```

GCP:

```bash
sops --encrypt --gcp-kms projects/<project>/locations/<location>/keyRings/<ring>/cryptoKeys/<key> secret.yaml > secret.sops.yaml
```

Azure:

```bash
sops --encrypt --azure-kv https://<vault>.vault.azure.net/keys/<key>/<version> secret.yaml > secret.sops.yaml
```

The controller process must have decrypt permissions through its runtime identity or mounted credentials.

## File Naming

GitOpsCTL does not require a specific encrypted filename suffix. Any `.yaml`, `.yml`, or `.json` file containing SOPS metadata can be decrypted.

Recommended convention:

```text
secret.sops.yaml
config.sops.json
```

## Safety Checklist

- Commit only encrypted secret files.
- Keep decryption keys and cloud credentials outside the repo.
- Run the controller with least-privilege decrypt permissions.
- Use Kubernetes RBAC and `allowed_namespaces` to limit blast radius.
- Rotate keys and re-encrypt secrets when access changes.
- Confirm decrypted files are not produced in your working tree before committing.

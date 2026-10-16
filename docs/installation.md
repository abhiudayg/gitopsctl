# Installation

GitOpsCTL is distributed as a single Go binary and can also run in a container.

## Requirements

- Go 1.25 or newer when building from source.
- `git` available on the machine running the controller.
- Network access to the target Git repositories.
- A kubeconfig that can reach each target Kubernetes cluster.
- Optional: SOPS provider tooling or credentials when using encrypted manifests.

## Prebuilt Binary

Download a release archive from the [GitHub Releases](https://github.com/aeswibon/gitopsctl/releases) page.

```bash
tar -xzf gitopsctl_<OS>_<ARCH>.tar.gz
chmod +x gitopsctl
sudo mv gitopsctl /usr/local/bin/gitopsctl
gitopsctl --help
```

Use the archive that matches your platform, for example Darwin arm64 for Apple Silicon macOS or Linux amd64 for most x86 Linux hosts.

## Install With Go

```bash
go install aeswibon.com/github/gitopsctl@latest
```

Make sure your Go binary directory is on `PATH`:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
gitopsctl --help
```

## Build From Source

```bash
git clone https://github.com/aeswibon/gitopsctl.git
cd gitopsctl
go build -o gitopsctl main.go
./gitopsctl --help
```

Run tests:

```bash
go test ./...
```

Run coverage:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
```

## Docker

Pull the image:

```bash
docker pull ghcr.io/aeswibon/gitopsctl:latest
```

Run the controller with local configs and kubeconfig mounted:

```bash
docker run --rm -it \
  -v "$HOME/.kube/config:/root/.kube/config:ro" \
  -v "$PWD/configs:/app/configs" \
  -p 8080:8080 \
  -e GITOPSCTL_API_TOKEN \
  ghcr.io/aeswibon/gitopsctl:latest \
  start --api-address 0.0.0.0:8080
```

Notes:

- `kubeconfigPath` inside `configs/clusters.json` must match the path inside the container, such as `/root/.kube/config`.
- Mount SOPS keys or cloud credentials when encrypted manifests need decryption.
- Mount `configs/` as writable because GitOpsCTL persists status back to JSON files.

## Shell Completion

Cobra can generate completion scripts if enabled in the binary. Check the current command help:

```bash
gitopsctl completion --help
```

If completion is not present in your build, use normal shell history and aliases until completion support is added.

## Verify Installation

```bash
gitopsctl --help
gitopsctl register-cluster --help
gitopsctl start --help
```

Then continue with [Getting Started](getting-started.md).

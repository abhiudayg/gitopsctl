# Contributing to GitOpsCTL

First off, thank you for considering contributing to GitOpsCTL! It's people like you that make GitOpsCTL such a great tool.

## Getting Started

### Prerequisites

- **Go 1.24+**: Ensure you have Go installed and matching the `go.mod` version.
- **Git**: For version control.
- **Kubernetes Cluster**: A running cluster (e.g., OrbStack, minikube, kind, or Docker Desktop) to test changes locally.

### Local Development Setup

1. **Fork the repository** on GitHub.
2. **Clone your fork** locally:
   ```bash
   git clone https://github.com/your-username/gitopsctl.git
   cd gitopsctl
   ```
3. **Install dependencies**:
   ```bash
   go mod tidy
   ```
4. **Build the CLI**:
   ```bash
   go build -o gitopsctl .
   ```
5. **Set up hooks**:
   We use `pre-commit` to ensure code quality. Coverage checks are enforced on push.
   ```bash
   pip install pre-commit
   pre-commit install --hook-type pre-commit --hook-type pre-push
   ```
6. **Run tests & check coverage**:
   ```bash
   go test -coverprofile=coverage.out ./...
   go tool cover -func=coverage.out
   ```
   Then run the success/failure scenarios in `.verify/` (needs `jq`); see `.verify/README.md`:
   ```bash
   .verify/run.sh --no-e2e
   ```



## How to Contribute

### 1. Find an Issue
Look for issues labeled `good first issue` or `help wanted` if you're not sure where to start. If you want to work on something specific, it's best to open an issue first to discuss it before spending time writing code.

### 2. Create a Branch
Create a branch for your feature or bug fix:
```bash
git checkout -b feature/my-awesome-feature
```

### 3. Make Changes
Write your code, making sure to follow standard Go conventions.
- Run `go fmt ./...` to format your code.
- Run `go vet ./...` to catch common mistakes.
- **Maintain Test Coverage**: Ensure all changes are covered by tests. The project enforces a minimum of **80% total coverage**.
- Ensure all existing and new tests pass.


### 4. Commit Messages
Write clear, concise commit messages. A good commit message should describe *what* was changed and *why*.
```text
feat(api): add a new endpoint for fetching logs

This adds a new `/api/v1/logs` endpoint to support the new dashboard UI.
Fixes #123
```

### 5. Submit a Pull Request
Push your branch to your fork and submit a Pull Request against the `main` branch of the upstream repository.
- Fill out the PR template completely.
- Ensure CI checks pass.
- Be responsive to feedback from reviewers.

## Documentation
If your change introduces new features, make sure to update the relevant documentation in the `docs/` folder or the `README.md`.

Thank you for your contributions!

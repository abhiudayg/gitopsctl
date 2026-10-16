# .verify

Executable definition of "this gitopsctl is good". It is meant to be run by agents and humans after any change, and answers one question: **is it safe to ship?**

```bash
.verify/run.sh                         # everything: gates, scenarios, e2e (if available)
.verify/run.sh --only S-SYNC-04,F-API-02   # just these scenarios while iterating
.verify/run.sh --no-gates --no-e2e     # scenarios only
.verify/run.sh --list                  # ids, kinds, severities, titles
```

The runner writes `.verify/report.json` and exits `0` only on verdict `PASS`.

## What is checked

| Layer | Where | What it proves |
|-------|-------|----------------|
| Gates | `scenarios.json` → `gates` | Build, vet, gofmt, golangci-lint, unit tests with `-race`, coverage ≥ 80%, tidy modules, govulncheck, GoReleaser config |
| Integration scenarios | `integration/` | The real `gitopsctl` binary against real git repositories and an in-process fake Kubernetes API (`integration/fakekube_test.go`): auth, sync, manual approval, plan, prune, outages, recovery, CLI validation, docs and release hygiene |
| Unit-backed scenarios | `scenarios.json` → `go-test` targets | Narrow behaviours that integration cannot reach cheaply: shutdown races, atomic state writes, prune guards |
| End-to-end | `e2e/kind.sh` | Real API-server behaviour on kind. Skipped (not passed) when `kind`, `kubectl` or a Docker daemon is missing |

Scenario ids: `S-*` are **success** scenarios (correct behaviour works), `F-*` are **failure** scenarios (bad input, outages and attacks are handled safely), `E-*` need a real cluster.

## Reading the report

```jsonc
{
  "summary": { "verdict": "PASS|CONDITIONAL|FAIL", "score": 98.01, "blocking_failures": ["S-PLAN-01"] },
  "results": [{ "id": "S-SYNC-01", "kind": "success", "severity": "critical", "status": "pass|fail|skipped", "detail": "…" }]
}
```

- **Score** = weight of passed ÷ weight of non-skipped, with critical=5, high=3, medium=2, low=1.
- **PASS**: no failed critical/high scenario and score ≥ 95. **CONDITIONAL**: no failed critical and score ≥ 80. **FAIL**: otherwise.
- `skipped` means the check could not run here, not that it passed. Report skips; do not count them as evidence.
- `detail` holds the failing test names and their output. A test named in the catalog but absent shows as `missing`, which fails the scenario.

## How an agent should use it

1. Run `.verify/run.sh` before changing anything to get a baseline, and again before reporting completion.
2. For each failure, read the scenario in `scenarios.json`: `given/when/then` is the contract and `if_failing` points at the likely cause.
3. Fix the product code. Do not edit a scenario, weaken an assertion, or lower a threshold to turn a failure green. Change a scenario only when the intended behaviour changed, and say so in the report.
4. A regression in any critical scenario blocks the change, whatever the score.

## Adding a scenario

1. Write the test. Integration tests live in `integration/` and are named `TestScenario_<ID with underscores>_<Description>` (`S-SYNC-01` → `TestScenario_S_SYNC_01_…`).
2. Add an entry to `scenarios.json` with `id`, `kind`, `severity`, `given/when/then`, `verify.targets` (package plus exact top-level test names) and `if_failing`.
3. Run `.verify/run.sh --only <ID>`; then break the product behaviour on purpose and confirm the scenario fails.

The fake API in `integration/fakekube_test.go` implements only ConfigMaps, discovery and `/version`. Use `e2e/kind.sh` for anything that depends on real server behaviour (defaulting, field managers, other kinds).

`go test ./...` ignores this directory because it starts with a dot; the runner invokes it explicitly with `go test ./.verify/integration`.

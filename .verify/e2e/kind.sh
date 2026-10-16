#!/usr/bin/env bash
# Real-cluster check for gitopsctl using kind. Prints "RESULT E-KIND-01 PASS|FAIL|SKIP <detail>".
set -uo pipefail

ID="E-KIND-01"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORK="$(mktemp -d)"
CLUSTER="gitopsctl-verify-$$"
PORT=$((20000 + RANDOM % 10000))
KUBECONFIG_FILE="$WORK/kubeconfig"
PID=""

for tool in kind kubectl docker jq git curl go; do
  command -v "$tool" >/dev/null 2>&1 || { echo "RESULT $ID SKIP missing tool: $tool"; exit 0; }
done
docker info >/dev/null 2>&1 || { echo "RESULT $ID SKIP docker daemon is not running"; exit 0; }

cleanup() {
  [[ -n "$PID" ]] && kill "$PID" >/dev/null 2>&1 && wait "$PID" >/dev/null 2>&1
  kind delete cluster --name "$CLUSTER" >/dev/null 2>&1
  rm -rf "$WORK"
}
trap cleanup EXIT

fail() { echo "RESULT $ID FAIL $1"; exit 0; }

kc() { kubectl --kubeconfig "$KUBECONFIG_FILE" "$@"; }

wait_for() { # description timeout command...
  local description="$1" timeout="$2"; shift 2
  local deadline=$((SECONDS + timeout))
  while (( SECONDS < deadline )); do
    "$@" >/dev/null 2>&1 && return 0
    sleep 1
  done
  fail "timed out waiting for: $description"
}

app_synced() {
  local status
  status="$(curl -fsS "http://127.0.0.1:$PORT/api/v1/applications/web" | jq -r .status)" || return 1
  [[ "$status" == "Synced" || "$status" == "Healthy" ]]
}

(cd "$ROOT" && go build -o "$WORK/gitopsctl" .) || fail "go build failed"
kind create cluster --name "$CLUSTER" --kubeconfig "$KUBECONFIG_FILE" --wait 120s >"$WORK/kind.log" 2>&1 || fail "kind create cluster failed: $(tail -n 3 "$WORK/kind.log")"

REPO="$WORK/repo"
mkdir -p "$REPO"
git -C "$REPO" init -q -b master
cm() { printf 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: %s\n  namespace: default\ndata:\n  key: v1\n' "$1"; }
cm keep > "$REPO/keep.yaml"
cm drop > "$REPO/drop.yaml"
git -C "$REPO" -c user.name=verify -c user.email=verify@example.com add -A
git -C "$REPO" -c user.name=verify -c user.email=verify@example.com commit -q -m init

mkdir -p "$WORK/run/configs"
cat > "$WORK/run/configs/clusters.json" <<JSON
[{"name":"kind","kubeconfig_path":"$KUBECONFIG_FILE","registered_at":"2026-01-01T00:00:00Z"}]
JSON
cat > "$WORK/run/configs/applications.json" <<JSON
[{"name":"web","repo_url":"$REPO","branch":"master","path":".","cluster_name":"kind","interval":"2s","status":"Pending","prune":true}]
JSON

(cd "$WORK/run" && exec "$WORK/gitopsctl" start --api-address "127.0.0.1:$PORT" >"$WORK/controller.log" 2>&1) &
PID=$!

wait_for "controller health" 30 curl -fsS "http://127.0.0.1:$PORT/health"
wait_for "app synced" 90 app_synced

kc get configmap keep drop -n default >/dev/null 2>&1 || fail "ConfigMaps were not created"
labels="$(kc get configmap keep -n default -o json | jq -r '.metadata.labels | [.["app.kubernetes.io/managed-by"], .["gitopsctl.io/app"], .["gitopsctl.io/cluster"]] | join(",")')"
[[ "$labels" == "gitopsctl,web,kind" ]] || fail "unexpected ownership labels: $labels"

plan="$(cd "$WORK/run" && "$WORK/gitopsctl" --api-url "http://127.0.0.1:$PORT" diff-app -n web 2>&1)"
grep -q "0 to create, 0 to update" <<<"$plan" || fail "diff-app does not report an in-sync app: $plan"

rm "$REPO/drop.yaml"
git -C "$REPO" -c user.name=verify -c user.email=verify@example.com commit -q -am "drop"
wait_for "drop pruned" 90 bash -c "! kubectl --kubeconfig '$KUBECONFIG_FILE' get configmap drop -n default"
kc get configmap keep -n default >/dev/null 2>&1 || fail "prune removed a resource that is still in Git"

echo "RESULT $ID PASS applied with ownership labels, diff clean, pruned only the removed resource"

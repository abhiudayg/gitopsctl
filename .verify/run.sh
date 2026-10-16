#!/usr/bin/env bash
# Runs the quality gates and success/failure scenarios defined in .verify/scenarios.json
# and writes a scored report to .verify/report.json.
#
# Usage: .verify/run.sh [--no-gates] [--no-scenarios] [--no-e2e] [--only ID[,ID...]] [--list]
# Exit status: 0 = PASS, 1 = CONDITIONAL or FAIL, 2 = harness error.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
CATALOG="$HERE/scenarios.json"
WORK="$HERE/.work"
REPORT="$HERE/report.json"
RUN_GATES=1
RUN_SCENARIOS=1
RUN_E2E=1
ONLY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-gates) RUN_GATES=0 ;;
    --no-scenarios) RUN_SCENARIOS=0 ;;
    --no-e2e) RUN_E2E=0 ;;
    --only) ONLY="${2:-}"; shift ;;
    --list)
      jq -r '(.gates[] | [.id, "gate", .severity, .title]), (.scenarios[] | [.id, .kind, .severity, .title]) | @tsv' "$CATALOG"
      exit 0 ;;
    -h|--help) sed -n '2,6p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
  shift
done

for tool in jq go git; do
  command -v "$tool" >/dev/null 2>&1 || { echo "required tool missing: $tool" >&2; exit 2; }
done

cd "$ROOT"
rm -rf "$WORK"
mkdir -p "$WORK"
RESULTS="$WORK/results.jsonl"
: > "$RESULTS"
MODULE="$(go list -m)"

record() {
  jq -cn --arg id "$1" --arg kind "$2" --arg severity "$3" --arg title "$4" --arg status "$5" \
    --argjson duration "$6" --arg detail "$7" \
    '{id:$id, kind:$kind, severity:$severity, title:$title, status:$status, duration_s:$duration, detail:$detail}' >> "$RESULTS"
}

tail_of() { tail -n "${2:-20}" "$1" | tail -c 2000; }

run_gates() {
  while IFS= read -r gate; do
    id="$(jq -r .id <<<"$gate")"
    title="$(jq -r .title <<<"$gate")"
    severity="$(jq -r .severity <<<"$gate")"
    command="$(jq -r .command <<<"$gate")"
    skip_tool="$(jq -r '.skip_if_missing // empty' <<<"$gate")"
    threshold="$(jq -r '.threshold // empty' <<<"$gate")"

    if [[ -n "$skip_tool" ]] && ! command -v "$skip_tool" >/dev/null 2>&1; then
      record "$id" gate "$severity" "$title" skipped 0 "$skip_tool is not installed"
      continue
    fi

    echo "[gate] $id: $title" >&2
    started=$(date +%s)
    if [[ "$command" == "coverage" ]]; then
      if [[ ! -s "$WORK/cover.out" ]]; then
        record "$id" gate "$severity" "$title" skipped 0 "no coverage profile (G-UNIT did not produce one)"
        continue
      fi
      total="$(go tool cover -func="$WORK/cover.out" | awk '/^total:/ {gsub("%","",$3); print $3}')"
      elapsed=$(( $(date +%s) - started ))
      if awk -v t="$total" -v th="$threshold" 'BEGIN{exit !(t+0 >= th+0)}'; then
        record "$id" gate "$severity" "$title" pass "$elapsed" "total coverage ${total}% (threshold ${threshold}%)"
      else
        record "$id" gate "$severity" "$title" fail "$elapsed" "total coverage ${total}% is below ${threshold}%"
      fi
      continue
    fi

    bash -c "$command" > "$WORK/$id.log" 2>&1
    code=$?
    elapsed=$(( $(date +%s) - started ))
    if [[ $code -eq 0 ]]; then
      record "$id" gate "$severity" "$title" pass "$elapsed" ""
    else
      record "$id" gate "$severity" "$title" fail "$elapsed" "exit $code: $(tail_of "$WORK/$id.log" 25)"
    fi
  done < <(jq -c '.gates[]' "$CATALOG")
}

run_go_scenarios() {
  idx=0
  while IFS=$'\t' read -r pkg race tests; do
    idx=$((idx + 1))
    echo "[tests] $pkg ($(tr '|' ' ' <<<"$tests" | wc -w | tr -d ' ') tests)" >&2
    args=(test -json -count=1)
    [[ "$race" == "true" ]] && args+=(-race)
    go "${args[@]}" -run "^(${tests})\$" "$pkg" > "$WORK/go-$idx.json" 2>&1
  done < <(jq -r --arg only "$ONLY" '
    [.scenarios[] | select(.verify.type=="go-test") | select($only=="" or (.id as $sid | $only|split(",")|index($sid)))
      | .verify as $v | $v.targets[] | {package, race: ($v.race // false), tests}]
    | group_by([.package, .race])[]
    | [.[0].package, (.[0].race|tostring), ([.[].tests[]] | unique | join("|"))] | @tsv' "$CATALOG")

  cat "$WORK"/go-*.json 2>/dev/null | jq -R -s -c 'split("\n") | map(fromjson?)' > "$WORK/events.json"
  [[ -s "$WORK/events.json" ]] || echo '[]' > "$WORK/events.json"

  jq -c --slurpfile ev "$WORK/events.json" --arg m "$MODULE" --arg only "$ONLY" '
    def pkgpath: if startswith("./") then $m + "/" + .[2:] else . end;
    ($ev[0]) as $events
    | ($events | map(select(.Test != null and (.Test | contains("/") | not)))) as $tests
    | ($tests | map(select(.Action=="pass" or .Action=="fail" or .Action=="skip")) | map({key: (.Package + "|" + .Test), value: .Action}) | from_entries) as $res
    | ($tests | map(select(.Action=="output")) | group_by(.Package + "|" + .Test)
        | map({key: (.[0].Package + "|" + .[0].Test), value: (map(.Output) | join("") | .[-1200:])}) | from_entries) as $out
    | ($events | map(select(.Test == null and .Action=="output")) | group_by(.Package)
        | map({key: .[0].Package, value: (map(.Output) | join("") | .[-800:])}) | from_entries) as $pkgout
    | .scenarios[] | select(.verify.type=="go-test") | select($only=="" or (.id as $sid | $only|split(",")|index($sid)))
    | . as $s
    | [$s.verify.targets[] | (.package | pkgpath) as $p | .tests[] | {test: ., pkg: $p, key: ($p + "|" + .)}] as $checks
    | ($checks | map(. + {result: ($res[.key] // "missing")})) as $r
    | {id, kind, severity, title,
       status: (if all($r[]; .result=="pass") then "pass" else "fail" end),
       duration_s: 0,
       detail: ($r | map(select(.result != "pass"))
                 | map("\(.test): \(.result)\n" + (if .result=="missing" then ($pkgout[.pkg] // "") else ($out[.key] // "") end))
                 | join("\n"))}' "$CATALOG" >> "$RESULTS"
}

run_e2e() {
  while IFS= read -r scenario; do
    id="$(jq -r .id <<<"$scenario")"
    title="$(jq -r .title <<<"$scenario")"
    severity="$(jq -r .severity <<<"$scenario")"
    script="$(jq -r .verify.script <<<"$scenario")"
    missing=""
    for tool in $(jq -r '.verify.skip_if_missing[]?' <<<"$scenario"); do
      command -v "$tool" >/dev/null 2>&1 || missing="$missing $tool"
    done
    if [[ -z "$missing" ]] && command -v docker >/dev/null 2>&1 && ! docker info >/dev/null 2>&1; then
      missing=" docker daemon"
    fi
    if [[ $RUN_E2E -eq 0 ]]; then
      record "$id" "$(jq -r .kind <<<"$scenario")" "$severity" "$title" skipped 0 "--no-e2e"
      continue
    fi
    if [[ -n "$missing" ]]; then
      record "$id" "$(jq -r .kind <<<"$scenario")" "$severity" "$title" skipped 0 "unavailable:${missing}"
      continue
    fi
    echo "[e2e] $id via $script" >&2
    bash "$ROOT/$script" > "$WORK/$id.log" 2>&1
    line="$(grep -E "^RESULT $id " "$WORK/$id.log" | tail -n 1)"
    verdict="$(awk '{print $3}' <<<"$line")"
    detail="$(cut -d' ' -f4- <<<"$line")"
    case "$verdict" in
      PASS) record "$id" "$(jq -r .kind <<<"$scenario")" "$severity" "$title" pass 0 "$detail" ;;
      SKIP) record "$id" "$(jq -r .kind <<<"$scenario")" "$severity" "$title" skipped 0 "$detail" ;;
      *) record "$id" "$(jq -r .kind <<<"$scenario")" "$severity" "$title" fail 0 "${detail:-no RESULT line}: $(tail_of "$WORK/$id.log" 20)" ;;
    esac
  done < <(jq -c --arg only "$ONLY" '.scenarios[] | select(.verify.type=="e2e") | select($only=="" or (.id as $sid | $only|split(",")|index($sid)))' "$CATALOG")
}

[[ $RUN_GATES -eq 1 && -z "$ONLY" ]] && run_gates
if [[ $RUN_SCENARIOS -eq 1 ]]; then
  run_go_scenarios
  run_e2e
fi

jq -s --slurpfile catalog "$CATALOG" \
   --arg commit "$(git rev-parse HEAD 2>/dev/null || echo unknown)" \
   --arg branch "$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo unknown)" \
   --arg dirty "$(git status --porcelain 2>/dev/null | grep -v '^?? .verify/' | wc -l | tr -d ' ')" \
   --arg now "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '
  . as $results
  | $catalog[0].severity_weights as $w
  | ($results | map(select(.status != "skipped"))) as $scored
  | ($scored | map($w[.severity]) | add // 0) as $total
  | ($scored | map(select(.status=="pass") | $w[.severity]) | add // 0) as $earned
  | (if $total == 0 then 0 else (($earned * 10000 / $total) | round / 100) end) as $score
  | ($results | map(select(.status=="fail"))) as $failed
  | ($failed | map(select(.severity=="critical" or .severity=="high")) | map(.id)) as $blocking
  | ($failed | map(select(.severity=="critical")) | map(.id)) as $critical
  | {
      generated_at: $now,
      git: {commit: $commit, branch: $branch, uncommitted_files: ($dirty | tonumber)},
      summary: {
        total: ($results | length),
        passed: ($results | map(select(.status=="pass")) | length),
        failed: ($failed | length),
        skipped: ($results | map(select(.status=="skipped")) | length),
        score: $score,
        verdict: (if ($blocking | length) == 0 and $score >= 95 then "PASS"
                  elif ($critical | length) == 0 and $score >= 80 then "CONDITIONAL"
                  else "FAIL" end),
        blocking_failures: $blocking
      },
      results: $results
    }' "$RESULTS" > "$REPORT"

jq -r '
  "VERDICT \(.summary.verdict)  score \(.summary.score)  pass \(.summary.passed)  fail \(.summary.failed)  skipped \(.summary.skipped)",
  (.results[] | "\(.status | ascii_upcase | .[0:4])  \(.id)  [\(.severity)]  \(.title)" + (if .status != "pass" and .detail != "" then "\n      " + (.detail | split("\n")[0:6] | join("\n      ")) else "" end))
' "$REPORT"
echo "report: $REPORT"

verdict="$(jq -r .summary.verdict "$REPORT")"
[[ "$verdict" == "PASS" ]]

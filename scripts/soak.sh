#!/usr/bin/env bash
# Local soak for success criterion 5: loadgen `mixed` at the 25 req/s cap for
# 15 minutes at 100 % sampling, under container memory limits. PASS needs no
# OOMKill and a dropped-span ratio under 0.1 %.
set -euo pipefail
cd "$(dirname "$0")/.."
DURATION="${DURATION:-15m}"
compose=(docker compose -f docker-compose.yml -f docker-compose.soak.yml)

"${compose[@]}" up -d --build
# Runs on every exit path (normal, `set -e`, or an interrupt bash can trap):
# dump logs first, then tear the whole stack down, then restore the exit code
# that triggered the trap (a plain `trap '...' EXIT` would otherwise exit with
# the status of the trap's own last command instead).
cleanup() {
  local status=$?
  "${compose[@]}" logs --tail=50 app worker > soak-logs.txt 2>&1 || true
  "${compose[@]}" down --remove-orphans || true
  exit "$status"
}
trap cleanup EXIT
atlas migrate apply --env local
make build
./bin/image-gallery loadgen --target http://localhost:8080 --scenario mixed --rate 25 --duration "$DURATION" --concurrency 40

sleep 30 # let the last export batches and one metrics interval land
fail=0
for c in image-gallery-app image-gallery-worker; do
  oom="$(docker inspect -f '{{.State.OOMKilled}}' "$c")"
  restarts="$(docker inspect -f '{{.RestartCount}}' "$c")"
  echo "$c OOMKilled=$oom restarts=$restarts"
  [[ "$oom" == "false" && "$restarts" == "0" ]] || fail=1
done
ended="$(curl -s http://localhost:8428/api/v1/query --data-urlencode 'query=sum(increase({__name__="telemetry.spans.ended"}[20m]))' | jq -r '.data.result[0].value[1] // "nan"')"
exported="$(curl -s http://localhost:8428/api/v1/query --data-urlencode 'query=sum(increase({__name__="telemetry.spans.exported",outcome="success"}[20m]))' | jq -r '.data.result[0].value[1] // "nan"')"
dropped="$(awk -v e="$ended" -v x="$exported" 'BEGIN { if (e == "nan" || x == "nan") { print "nan" } else { printf "%.0f", e - x } }')"
q='1 - sum(increase({__name__="telemetry.spans.exported",outcome="success"}[20m])) / sum(increase({__name__="telemetry.spans.ended"}[20m]))'
ratio="$(curl -s http://localhost:8428/api/v1/query --data-urlencode "query=$q" | jq -r '.data.result[0].value[1] // "nan"')"
echo "telemetry.spans.ended: $ended"
echo "telemetry.spans.exported (success): $exported"
echo "dropped spans: $dropped"
echo "dropped-span ratio: $ratio (limit 0.001)"
awk -v r="$ratio" 'BEGIN { exit !(r != "nan" && r < 0.001) }' || fail=1
[[ $fail -eq 0 ]] && echo "SOAK PASS" || { echo "SOAK FAIL"; exit 1; }

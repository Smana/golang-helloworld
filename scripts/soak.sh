#!/usr/bin/env bash
# Local soak for success criterion 5: loadgen `mixed` at the 25 req/s cap for
# 15 minutes at 100 % sampling, under container memory limits. PASS needs no
# OOMKill and a dropped-span ratio under 0.1 %.
set -euo pipefail
cd "$(dirname "$0")/.."
DURATION="${DURATION:-15m}"
compose=(docker compose -f docker-compose.yml -f docker-compose.soak.yml)

"${compose[@]}" up -d --build
trap '"${compose[@]}" logs --tail=50 app worker > soak-logs.txt 2>&1 || true' EXIT
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
q='1 - sum(increase({__name__="telemetry.spans.exported",outcome="success"}[20m])) / sum(increase({__name__="telemetry.spans.ended"}[20m]))'
ratio="$(curl -s http://localhost:8428/api/v1/query --data-urlencode "query=$q" | jq -r '.data.result[0].value[1] // "nan"')"
echo "dropped-span ratio: $ratio (limit 0.001)"
awk -v r="$ratio" 'BEGIN { exit !(r != "nan" && r < 0.001) }' || fail=1
[[ $fail -eq 0 ]] && echo "SOAK PASS" || { echo "SOAK FAIL"; exit 1; }

#!/bin/bash
set -uo pipefail

BASE=${BASE_URL:-http://localhost:8080}
DURATION=${DURATION:-30}   # seconds
PARALLEL=${PARALLEL:-5}    # parallel rounds

worker() {
  local end=$((SECONDS + DURATION))
  while (( SECONDS < end )); do
    case $((RANDOM % 10)) in
      0|1|2|3|4|5) curl -s -o /dev/null "$BASE/tasks?limit=20" ;;
      6|7)         curl -s -o /dev/null -X POST -H 'Content-Type: application/json' \
                        -d '{"title":"bash task","status":"todo"}' "$BASE/tasks" ;;
      8)           curl -s -o /dev/null "$BASE/tasks/00000000-0000-0000-0000-000000000000" ;; # 404
      9)           curl -s -o /dev/null "$BASE/unknown" ;;                                    # unknown
    esac
    sleep 0.05
  done
}

for _ in $(seq "$PARALLEL"); do worker & done
wait

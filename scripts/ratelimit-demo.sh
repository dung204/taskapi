#!/bin/bash
# Sends traffic that trips the rate limiter, so the "Rate limiting" row of the
# Grafana dashboard (grafana/dashboards/taskapi.json) has something to show.
#
# RPS and BURST must match the server's RATE_LIMIT_RPS and RATE_LIMIT_BURST.
# The limiter keys clients by source IP. When the target is local, each client
# binds its own 127.0.0.N address, so "Tracked clients" goes above 1. Behind
# docker or a proxy the server sees one address, and that panel stays at 1.
set -uo pipefail

BASE=${BASE_URL:-http://localhost:8080}
RPS=${RPS:-10}            # RATE_LIMIT_RPS of the server
BURST=${BURST:-20}        # RATE_LIMIT_BURST of the server
CLIENTS=${CLIENTS:-5}     # distinct source IPs in the last phase
DURATION=${DURATION:-20}  # seconds per timed phase
GAP=${GAP:-5}             # seconds of silence between phases

host=${BASE#*://}
host=${host%%[:/]*}
local_ips=0
if [[ $host == localhost || $host == 127.* ]]; then
  local_ips=1
  BASE=${BASE/localhost/127.0.0.1} # an IPv4 source address cannot reach ::1
fi

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT
trap 'trap - INT TERM; kill 0' INT TERM

mul() { awk -v a="$1" -v b="$2" 'BEGIN { print a * b }'; }

source_ip() { (( local_ips )) && echo "127.0.0.$1"; }

# request <source-ip>: sends one request to a random endpoint, prints its status.
request() {
  local iface=()
  [[ -n ${1:-} ]] && iface=(--interface "$1")
  local curl=(curl -s -o /dev/null -w '%{http_code}\n' ${iface[@]+"${iface[@]}"})

  case $((RANDOM % 10)) in
    0|1|2|3|4|5) "${curl[@]}" "$BASE/tasks?limit=20" ;;
    6|7)         "${curl[@]}" -X POST -H 'Content-Type: application/json' \
                   -d '{"title":"ratelimit demo","status":"todo"}' "$BASE/tasks" ;;
    8|9)         "${curl[@]}" "$BASE/tasks/00000000-0000-0000-0000-000000000000" ;; # 404
  esac
}

# client <source-ip> <requests per second> <seconds> <outfile>
client() {
  local pause end=$((SECONDS + $3))
  pause=$(awk -v r="$2" 'BEGIN { printf "%.3f", 1 / r }')
  while (( SECONDS < end )); do
    request "$1" >>"$4"
    sleep "$pause"
  done
}

summarize() { # <label> <outfile>
  printf '  %-34s' "$1"
  sort "$2" | uniq -c | awk '{ printf " %s=%s", $2, $1 }'
  echo
}

if ! curl -sf -o /dev/null "$BASE/healthz"; then
  echo "server is not reachable at $BASE, start it first (see BASE_URL)" >&2
  exit 1
fi

echo "target $BASE, limit ${RPS} req/s per client, burst $BURST"
echo

echo "1/4 under the limit: $(mul "$RPS" 0.5) req/s for ${DURATION}s, expect only 2xx/404"
client "$(source_ip 1)" "$(mul "$RPS" 0.5)" "$DURATION" "$out/under"
sleep "$GAP"

echo "2/4 burst: $((BURST * 2)) back-to-back requests, expect about $BURST through, then 429"
for _ in $(seq $((BURST * 2))); do request "$(source_ip 1)"; done >"$out/burst"
retry_after=$(curl -si ${local_ips:+--interface 127.0.0.1} "$BASE/tasks" | tr -d '\r' | awk 'tolower($1) == "retry-after:" { print $2 }')
sleep "$GAP"

echo "3/4 overload: $(mul "$RPS" 3) req/s for ${DURATION}s from one client, expect about 2/3 to be 429"
client "$(source_ip 1)" "$(mul "$RPS" 3)" "$DURATION" "$out/overload"
sleep "$GAP"

echo "4/4 $CLIENTS clients: $(mul "$RPS" 1.5) req/s each for ${DURATION}s, expect about 1/3 to be 429"
for i in $(seq "$CLIENTS"); do
  client "$(source_ip "$i")" "$(mul "$RPS" 1.5)" "$DURATION" "$out/clients" &
done
wait

echo
echo "status codes seen by the client:"
summarize "1 under the limit" "$out/under"
summarize "2 burst" "$out/burst"
summarize "3 overload" "$out/overload"
summarize "4 $CLIENTS clients" "$out/clients"
echo
if [[ -n $retry_after ]]; then
  echo "Retry-After on a rejected request: ${retry_after}s"
else
  echo "no 429 was seen: check that RATE_LIMIT_RPS is not 0 and matches RPS/BURST here"
fi

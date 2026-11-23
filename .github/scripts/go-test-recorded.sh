#!/usr/bin/env bash
# go-test-recorded.sh: `go test -count=1 ./...` behind a local proxy that only
# records where each connection was going, and fails if anything in the
# tests asked for app.rosmarops.com. RosmarOps reaches the network only
# through HTTPS_PROXY when it is set, so a test that makes the program send
# usage statistics or a problem list to production by mistake shows up here
# as «CONNECT app.rosmarops.com:443», and the send itself never leaves this
# machine (the recorder never opens a tunnel). Tests that check a send on
# purpose set their own local proxy, which takes precedence.
set -euo pipefail

work="$(mktemp -d)"
log="$work/connects.log"
go build -o "$work/proxyrecorder" ./tests/trust/testdata/proxyrecorder
"$work/proxyrecorder" >"$log" &
recorder=$!
trap 'kill "$recorder" 2>/dev/null || true' EXIT

for _ in $(seq 1 50); do
  grep -q '^ADDR ' "$log" && break
  sleep 0.1
done
addr="$(sed -n 's/^ADDR //p' "$log" | head -1)"
if [ -z "$addr" ]; then
  echo "the recording proxy did not start" >&2
  exit 1
fi
proxy="http://$addr"
echo "recording proxy: $proxy"

status=0
HTTPS_PROXY="$proxy" https_proxy="$proxy" HTTP_PROXY="$proxy" http_proxy="$proxy" \
  NO_PROXY="" no_proxy="" GOPROXY=off \
  go test -count=1 ./... || status=$?

echo "connections the tests asked for (through the recording proxy):"
grep '^CONNECT ' "$log" || echo "  none"
if grep -qi '^CONNECT app\.rosmarops\.com' "$log"; then
  echo "::error::a test asked for app.rosmarops.com: it would have sent to production" >&2
  exit 1
fi
exit "$status"

#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
engine="${HOSTOPS_CONTAINER_ENGINE:-docker}"
image="localhost/hostops-systemd-lab:latest"
if [[ "${1:-}" != --no-build ]]; then
  "$engine" build -t "$image" -f scripts/linux-lab.Containerfile .
fi
# Privileges are confined to this disposable container so systemd can exercise
# the actual sandbox. No ports or host directories are mounted or published.
container="$("$engine" run -d --privileged --network none --cgroupns=private \
  --tmpfs /run --tmpfs /tmp --label hostops.disposable=true "$image")"
[[ "$container" =~ ^[0-9a-f]{12,64}$ ]]
trap '"$engine" rm -f "$container" >/dev/null' EXIT
for ((i=0;i<30;i++)); do
  state="$("$engine" exec "$container" systemctl is-system-running 2>/dev/null || true)"
  if [[ "$state" == running || "$state" == degraded ]]; then break; fi
  sleep 1
done
"$engine" exec "$container" touch /run/hostops-disposable-lab
"$engine" exec "$container" go test -count=1 -race ./...
"$engine" exec -e HOSTOPS_SYSTEMD_LAB=1 "$container" go test -count=1 -v \
  ./internal/collector -run '^TestVPSAgentDisposableSystemdLifecycle$'
"$engine" exec "$container" go test ./internal/discovery -run '^$' \
  -fuzz '^FuzzProxyConfigurationParsers$' -fuzztime 10s -parallel 2

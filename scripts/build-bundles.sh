#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

# Every workstation bundle contains both supported Linux collector architectures.
# Build into a new directory to avoid carrying stale executables into archives.
mkdir -p output/bundles
stage="$(mktemp -d "$PWD/output/bundles/.build-XXXXXXXX")"
case "$stage" in "$PWD/output/bundles/.build-"*) ;; *) exit 1 ;; esac
[[ -d "$stage" && ! -L "$stage" ]]
trap 'rm -rf -- "$stage"' EXIT
for target in windows-amd64 linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  os="${target%-*}"
  arch="${target##*-}"
  dir="$stage/hostops-$target"
  mkdir -p "$dir"
  suffix=""
  if [[ "$os" == windows ]]; then suffix=".exe"; fi
  for program in hostops hostops-agent hostops-watcher; do
    GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -o "$dir/$program$suffix" "./cmd/$program"
  done
  for remote_arch in amd64 arm64; do
    mkdir -p "$dir/collectors/linux-$remote_arch" "$dir/agents/vps/linux-$remote_arch"
    GOOS=linux GOARCH="$remote_arch" CGO_ENABLED=0 go build -trimpath -o "$dir/collectors/linux-$remote_arch/hostops" ./cmd/hostops
    GOOS=linux GOARCH="$remote_arch" CGO_ENABLED=0 go build -trimpath -o "$dir/agents/vps/linux-$remote_arch/hostops-vps-agent" ./cmd/hostops-vps-agent
  done
  cp LICENSE README.md "$dir/"
  cp -R docs examples "$dir/"
  tar -czf "output/bundles/hostops-$target.tar.gz" -C "$stage" "hostops-$target"
done
cd output/bundles
sha256sum hostops-*.tar.gz > SHA256SUMS

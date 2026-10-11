#!/usr/bin/env bash
set -euo pipefail
project="$(cd -- "$(dirname -- "$0")/.." && pwd -P)"
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) exit 1 ;; esac
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) exit 1 ;; esac
bundle="${1:-$project/output/bundles/hostops-$platform-$arch.tar.gz}"
mkdir -p "$project/local"
test_root="$(mktemp -d "$project/local/download-smoke-XXXXXXXX")"
mkdir -p "$test_root/mock-bin"
export HOSTOPS_TEST_BUNDLE="$bundle" HOSTOPS_TEST_SUMS="$test_root/SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then digest="$(sha256sum "$bundle" | cut -d ' ' -f 1)"; else digest="$(shasum -a 256 "$bundle" | cut -d ' ' -f 1)"; fi
printf '%s  %s\n' "$digest" "$(basename "$bundle")" > "$HOSTOPS_TEST_SUMS"
cat > "$test_root/mock-bin/curl" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
url= output= latest=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --proto|--proto-redir) shift 2 ;;
    -fsSL) shift ;;
    -o) output="$2"; shift 2 ;;
    -w) latest=1; shift 2 ;;
    https://*) url="$1"; shift ;;
    *) exit 1 ;;
  esac
done
if [[ $latest = 1 && "$url" = https://github.com/Shumair99/host-ops/releases/latest ]]; then
  printf 'https://github.com/Shumair99/host-ops/releases/tag/v1.2.3'
elif [[ "$url" = "https://github.com/Shumair99/host-ops/releases/download/v1.2.3/$(basename "$HOSTOPS_TEST_BUNDLE")" ]]; then
  cp -- "$HOSTOPS_TEST_BUNDLE" "$output"
elif [[ "$url" = https://github.com/Shumair99/host-ops/releases/download/v1.2.3/SHA256SUMS ]]; then
  cp -- "$HOSTOPS_TEST_SUMS" "$output"
else exit 1; fi
MOCK
chmod 755 "$test_root/mock-bin/curl"
env PATH="$test_root/mock-bin:$PATH" bash "$project/scripts/install-latest.sh" --prefix "$test_root/installed" --bin "$test_root/bin" --no-path
"$test_root/bin/hostops" --version
before="$(readlink "$test_root/bin/hostops")"
printf '%064d  %s\n' 0 "$(basename "$bundle")" > "$HOSTOPS_TEST_SUMS"
if env PATH="$test_root/mock-bin:$PATH" bash "$project/scripts/install-latest.sh" --prefix "$test_root/installed" --bin "$test_root/bin" --no-path > "$test_root/rejected.txt" 2>&1; then
  printf 'Damaged download was accepted.\n' >&2; exit 1
fi
grep -q 'Download checksum failed' "$test_root/rejected.txt"
[[ "$(readlink "$test_root/bin/hostops")" = "$before" ]]
printf 'Latest-release installer passed: matching bundle and checksum rejection. No network requests or profile changes.\n'

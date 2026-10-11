#!/usr/bin/env bash
set -euo pipefail
project="$(cd -- "$(dirname -- "$0")/.." && pwd -P)"
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) exit 1 ;; esac
case "$(uname -s)" in Linux) platform=linux ;; Darwin) platform=darwin ;; *) exit 1 ;; esac
bundle="${1:-$project/output/bundles/hostops-$platform-$arch.tar.gz}"
if [[ "${HOSTOPS_INSTALL_TEST_CHILD:-}" != 1 ]]; then
  mkdir -p "$project/local"
  test_root="$(mktemp -d "$project/local/install-smoke-XXXXXXXX")"
  mkdir -p "$test_root/profile"
  env -u HOSTOPS_CONFIG HOME="$test_root/profile" XDG_CONFIG_HOME="$test_root/profile/config" XDG_CACHE_HOME="$test_root/profile/cache" HOSTOPS_INSTALL_TEST_CHILD=1 bash "$0" "$bundle" "$test_root"
  exit
fi
test_root="$2"
mkdir -p "$test_root/extracted" "$test_root/another-folder"
tar -xzf "$bundle" -C "$test_root/extracted"
source_dir="$test_root/extracted/hostops-$platform-$arch"
installation="$test_root/installed"; bin_dir="$test_root/bin"
bash "$source_dir/install.sh" --prefix "$installation" --bin "$bin_dir" --no-path
cd "$test_root/another-folder"
"$bin_dir/hostops" --version > "$test_root/version.txt"
"$bin_dir/hostops" start --demo --once --page export-ai --color never > "$test_root/dashboard.txt"
grep -q 'Export for AI' "$test_root/dashboard.txt"
grep -q 'DEMO' "$test_root/dashboard.txt"
if printf '\n' | "$bin_dir/hostops" start --once --db :memory: > "$test_root/onboarding.txt" 2>&1; then printf 'Expected cancelled setup.\n' >&2; exit 1; fi
grep -q 'First-time setup' "$test_root/onboarding.txt"
"$bin_dir/hostops" export-ai --demo --output "$test_root/export.md" --json > "$test_root/export.json"
grep -q 'security' "$test_root/export.md"
grep -q 'recovery' "$test_root/export.md"
settings="$XDG_CONFIG_HOME/HostOps/hostops.json"
[[ ! -e "$settings" ]]
mkdir -p "$(dirname "$settings")"
printf '%s\n' '{"host":{"id":"test-host","name":"Test connection","ssh":"ops@example.test","history_mode":"session"},"sites":[]}' > "$settings"
cp "$settings" "$test_root/settings-before.json"
bash "$source_dir/install.sh" --prefix "$installation" --bin "$bin_dir" --no-path
cmp "$settings" "$test_root/settings-before.json"
link_before="$(readlink "$bin_dir/hostops")"
cp "$source_dir/GETTING-STARTED.md" "$test_root/guide-before.md"
printf 'tampered\n' >> "$source_dir/GETTING-STARTED.md"
if bash "$source_dir/install.sh" --prefix "$installation" --bin "$bin_dir" --no-path > "$test_root/rejected.txt" 2>&1; then printf 'Damaged bundle accepted.\n' >&2; exit 1; fi
grep -q 'Checksum failed' "$test_root/rejected.txt"
[[ "$(readlink "$bin_dir/hostops")" = "$link_before" ]]
cp "$test_root/guide-before.md" "$source_dir/GETTING-STARTED.md"
"$bin_dir/hostops" start --once --no-collect --db :memory: --color never > "$test_root/reopened.txt"
grep -q 'Opening saved VPS connection: Test connection' "$test_root/reopened.txt"
! grep -q 'First-time setup' "$test_root/reopened.txt"
bash "$installation/uninstall.sh"
[[ ! -e "$installation" && ! -e "$bin_dir/hostops" && -f "$settings" && -f "$test_root/export.md" ]]
printf 'PASS: fresh install, command symlinks, onboarding entry, dashboard, export, repeat install, settings reuse, damaged-bundle rejection, safe uninstall.\nNo production VPS or real user profile was changed.\n' > "$test_root/RESULTS.txt"
case "$test_root" in "$project/local/install-smoke-"*) [[ ! -L "$test_root/extracted" ]] && rm -rf -- "$test_root/extracted" ;; *) exit 1 ;; esac
printf '\nInstallation smoke test passed. Evidence: %s\n' "$test_root"

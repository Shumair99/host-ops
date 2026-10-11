#!/usr/bin/env bash
set -euo pipefail

source_dir="$(cd -- "$(dirname -- "$0")" && pwd -P)"
case "$(uname -s)" in
  Linux) prefix="${XDG_DATA_HOME:-$HOME/.local/share}/hostops"; expected_os=linux ;;
  Darwin) prefix="$HOME/Library/Application Support/HostOps"; expected_os=darwin ;;
  *) printf 'Use the installer for your operating system.\n' >&2; exit 1 ;;
esac
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) printf 'HostOps supports amd64 and arm64.\n' >&2; exit 1 ;; esac
bin_dir="$HOME/.local/bin"
no_path=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --prefix) prefix="$2"; shift 2 ;;
    --bin) bin_dir="$2"; shift 2 ;;
    --no-path) no_path=1; shift ;;
    *) printf 'Usage: bash install.sh [--prefix DIRECTORY] [--bin DIRECTORY] [--no-path]\n' >&2; exit 1 ;;
  esac
done
[[ "$prefix" = /* && "$bin_dir" = /* && "$prefix" != / && "$bin_dir" != / ]] || { printf 'Use absolute, dedicated installation paths.\n' >&2; exit 1; }
[[ "$prefix" != *$'\n'* && "$bin_dir" != *$'\n'* && "$bin_dir" != *"'"* ]] || { printf 'Installation paths contain unsupported characters.\n' >&2; exit 1; }
[[ ! -L "$prefix" && ! -L "$bin_dir" ]] || { printf 'Installation directories cannot be links.\n' >&2; exit 1; }
if [[ -d "$prefix" && -n "$(ls -A -- "$prefix")" ]]; then
  [[ -f "$prefix/.hostops-install" && "$(head -n 1 "$prefix/.hostops-install")" = HostOps && "$(sed -n '2p' "$prefix/.hostops-install")" = "$prefix" ]] || { printf 'Choose an empty directory or an existing HostOps installation.\n' >&2; exit 1; }
fi
[[ -f "$source_dir/bundle.json" && -f "$source_dir/CHECKSUMS.sha256" ]]
grep -q '"application": "HostOps"' "$source_dir/bundle.json"
grep -q "\"target\": \"$expected_os-$arch\"" "$source_dir/bundle.json" || { printf 'This bundle does not match your computer architecture.\n' >&2; exit 1; }
hash_file() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum -- "$1" | cut -d ' ' -f 1
  else shasum -a 256 -- "$1" | cut -d ' ' -f 1; fi
}
# Validate all assets before writing any installation files. No jq, Python or Go required.
declare -a assets=()
while IFS= read -r line; do
  digest="${line%%  *}"; relative="${line#*  }"
  [[ "$digest" =~ ^[a-f0-9]{64}$ && -n "$relative" && "$relative" != /* && "$relative" != *\\* ]]
  case "/$relative/" in */../*|*/./*) printf 'Invalid bundle path.\n' >&2; exit 1 ;; esac
  [[ -f "$source_dir/$relative" && ! -L "$source_dir/$relative" ]]
  parent="$(dirname -- "$relative")"
  while [[ "$parent" != . ]]; do [[ ! -L "$source_dir/$parent" ]]; parent="$(dirname -- "$parent")"; done
  [[ "$(hash_file "$source_dir/$relative")" = "$digest" ]] || { printf 'Checksum failed: %s. Extract a fresh complete bundle.\n' "$relative" >&2; exit 1; }
  assets+=("$relative")
done < "$source_dir/CHECKSUMS.sha256"
for required in hostops hostops-agent hostops-watcher uninstall.sh bundle.json collectors/linux-amd64/hostops collectors/linux-arm64/hostops agents/vps/linux-amd64/hostops-vps-agent agents/vps/linux-arm64/hostops-vps-agent; do
  found=0; for relative in "${assets[@]}"; do [[ "$relative" != "$required" ]] || found=1; done
  [[ $found = 1 ]] || { printf 'Incomplete bundle: %s\n' "$required" >&2; exit 1; }
done
mkdir -p -- "$prefix" "$bin_dir"
prefix="$(cd -- "$prefix" && pwd -P)"; bin_dir="$(cd -- "$bin_dir" && pwd -P)"
[[ "$prefix" != "$source_dir" && "$prefix" != "$source_dir/"* ]] || { printf 'Install outside the extracted bundle.\n' >&2; exit 1; }
[[ ! -L "$prefix/versions" ]]
mkdir -p -- "$prefix/versions"
digest="$(hash_file "$source_dir/bundle.json")"; digest="${digest:0:20}"
release="$prefix/versions/$digest"
# Refuse to overwrite a command belonging to another installation/program.
for program in hostops hostops-agent hostops-watcher; do
  if [[ -e "$bin_dir/$program" || -L "$bin_dir/$program" ]]; then
    [[ -L "$bin_dir/$program" ]] || { printf 'Existing command is not a HostOps link: %s\n' "$bin_dir/$program" >&2; exit 1; }
    case "$(readlink "$bin_dir/$program")" in "$prefix/versions/"*/"$program") ;; *) printf 'Existing command belongs to another installation.\n' >&2; exit 1 ;; esac
  fi
done
stage="$(mktemp -d "$prefix/.stage-XXXXXXXX")"
cleanup() { case "$stage" in "$prefix/.stage-"*) [[ ! -L "$stage" ]] && rm -rf -- "$stage" ;; esac; }
trap cleanup EXIT
for relative in "${assets[@]}"; do
  mkdir -p -- "$stage/$(dirname -- "$relative")"
  cp -- "$source_dir/$relative" "$stage/$relative"
done
cp -- "$source_dir/CHECKSUMS.sha256" "$stage/CHECKSUMS.sha256"
chmod 755 "$stage/hostops" "$stage/hostops-agent" "$stage/hostops-watcher" "$stage/install.sh" "$stage/uninstall.sh" "$stage/collectors/"linux-*/hostops "$stage/agents/vps/"linux-*/hostops-vps-agent
if [[ -e "$release" || -L "$release" ]]; then
  [[ -d "$release" && ! -L "$release" ]]
  for relative in "${assets[@]}"; do
    [[ -f "$release/$relative" && ! -L "$release/$relative" && "$(hash_file "$release/$relative")" = "$(hash_file "$stage/$relative")" ]] || { printf 'Installed version is damaged. Choose another installation directory.\n' >&2; exit 1; }
  done
else mv -- "$stage" "$release"; fi
for program in hostops hostops-agent hostops-watcher; do ln -sfn -- "$release/$program" "$bin_dir/$program"; done
cp -- "$release/uninstall.sh" "$prefix/uninstall.sh"
printf 'HostOps\n%s\n%s\n' "$prefix" "$bin_dir" > "$prefix/.hostops-install"
if [[ $no_path = 0 ]]; then
  case "${SHELL:-/bin/bash}" in
    */zsh) profiles=("$HOME/.zshrc") ;;
    *) profiles=("$HOME/.bashrc" "$HOME/.profile"); [[ "$expected_os" != darwin ]] || profiles+=("$HOME/.bash_profile") ;;
  esac
  path_line="export PATH='$bin_dir':\"\$PATH\" # HostOps"
  for profile in "${profiles[@]}"; do
    if [[ ! -e "$profile" ]] || ! grep -Fqx -- "$path_line" "$profile"; then printf '\n%s\n' "$path_line" >> "$profile"; fi
  done
fi
printf '\nHostOps installed for your account.\nLocation: %s\nOpen a new terminal and run: hostops start\n' "$prefix"
command -v ssh >/dev/null 2>&1 || printf 'Install the OpenSSH client before connecting a VPS.\n'

#!/usr/bin/env bash
set -euo pipefail

repository=Shumair99/host-ops
case "$(uname -s)" in
  Linux) platform=linux ;;
  Darwin) platform=darwin ;;
  *) printf 'Use the Windows PowerShell installer on Windows.\n' >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) architecture=amd64 ;;
  aarch64|arm64) architecture=arm64 ;;
  *) printf 'HostOps supports amd64 and arm64.\n' >&2; exit 1 ;;
esac
for tool in curl tar; do
  command -v "$tool" >/dev/null 2>&1 || { printf 'Install %s before installing HostOps.\n' "$tool" >&2; exit 1; }
done
if command -v sha256sum >/dev/null 2>&1; then
  hash_file() { sha256sum -- "$1" | cut -d ' ' -f 1; }
elif command -v shasum >/dev/null 2>&1; then
  hash_file() { shasum -a 256 -- "$1" | cut -d ' ' -f 1; }
else
  printf 'Install sha256sum or shasum before installing HostOps.\n' >&2; exit 1
fi
latest="https://github.com/$repository/releases/latest"
release_url="$(curl --proto '=https' --proto-redir '=https' -fsSL -o /dev/null -w '%{url_effective}' "$latest")" || {
  printf 'Could not find the latest HostOps release. Check your connection and %s/releases.\n' "https://github.com/$repository" >&2; exit 1
}
tag="${release_url#https://github.com/$repository/releases/tag/}"
[[ "$tag" =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]] || { printf 'The latest release must be a stable version.\n' >&2; exit 1; }
name="hostops-$platform-$architecture.tar.gz"
base="https://github.com/$repository/releases/download/$tag"
work="$(mktemp -d "${TMPDIR:-/tmp}/hostops-download-XXXXXXXX")"
cleanup() { [[ -d "$work" && ! -L "$work" ]] && rm -rf -- "$work"; }
trap cleanup EXIT
printf 'Downloading HostOps %s for %s-%s...\n' "$tag" "$platform" "$architecture"
curl --proto '=https' --proto-redir '=https' -fsSL "$base/$name" -o "$work/$name"
curl --proto '=https' --proto-redir '=https' -fsSL "$base/SHA256SUMS" -o "$work/SHA256SUMS"
expected="$(awk -v name="$name" '$2 == name && length($1) == 64 && $1 !~ /[^a-f0-9]/ { print $1 }' "$work/SHA256SUMS")"
[[ "$expected" =~ ^[a-f0-9]{64}$ && "$(hash_file "$work/$name")" = "$expected" ]] || {
  printf 'Download checksum failed. Installation was not changed.\n' >&2; exit 1
}
# Reject links and paths outside the expected bundle before extracting it.
tar -tzf "$work/$name" > "$work/entries"
while IFS= read -r entry; do
  [[ "$entry" = "hostops-$platform-$architecture/"* && "$entry" != *\\* ]] || { printf 'Invalid archive entry.\n' >&2; exit 1; }
  case "/$entry/" in */../*|*/./*) printf 'Invalid archive path.\n' >&2; exit 1 ;; esac
done < "$work/entries"
[[ -n "$(cat "$work/entries")" ]]
tar -tvzf "$work/$name" > "$work/types"
awk 'substr($0,1,1) != "-" && substr($0,1,1) != "d" { bad=1 } END { exit bad }' "$work/types" || { printf 'Archive contains unsupported links or special files.\n' >&2; exit 1; }
mkdir "$work/extracted"
tar -xzf "$work/$name" -C "$work/extracted"
bash "$work/extracted/hostops-$platform-$architecture/install.sh" "$@"
printf 'Ready. Run: hostops start\n'

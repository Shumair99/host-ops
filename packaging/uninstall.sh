#!/usr/bin/env bash
set -euo pipefail
prefix="$(cd -- "$(dirname -- "$0")" && pwd -P)"
[[ "$prefix" != / && -f "$prefix/.hostops-install" && ! -L "$prefix/.hostops-install" ]]
[[ "$(head -n 1 "$prefix/.hostops-install")" = HostOps && "$(sed -n '2p' "$prefix/.hostops-install")" = "$prefix" ]] || { printf 'Not a managed HostOps installation.\n' >&2; exit 1; }
bin_dir="$(sed -n '3p' "$prefix/.hostops-install")"
[[ "$bin_dir" = /* && "$bin_dir" != / && ! -L "$bin_dir" ]]
for program in hostops hostops-agent hostops-watcher; do
  if [[ -L "$bin_dir/$program" ]]; then
    case "$(readlink "$bin_dir/$program")" in "$prefix/versions/"*/"$program") rm -- "$bin_dir/$program" ;; esac
  fi
done
path_line="export PATH='$bin_dir':\"\$PATH\" # HostOps"
for profile in "$HOME/.bashrc" "$HOME/.profile" "$HOME/.bash_profile" "$HOME/.zshrc"; do
  if [[ -f "$profile" && ! -L "$profile" ]] && grep -Fqx -- "$path_line" "$profile"; then
    temporary="$(mktemp "${profile}.hostops-XXXXXXXX")"
    grep -Fvx -- "$path_line" "$profile" > "$temporary" || [[ $? = 1 ]]
    cat -- "$temporary" > "$profile"; rm -- "$temporary"
  fi
done
# Only the absolute directory verified against its own installation marker is removed.
rm -rf -- "$prefix"
printf 'HostOps removed. Your connection settings, history and exports remain in your user profile.\n'

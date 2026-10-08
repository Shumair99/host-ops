#!/bin/sh
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH

host_id=$1
ssh_user=$2
unit_hash=$3
case "$host_id" in [A-Za-z0-9]* ) : ;; *) exit 2 ;; esac
case "$host_id" in *[!A-Za-z0-9_.-]*|'') exit 2 ;; esac
case "$ssh_user" in [A-Za-z_]* ) : ;; *) exit 2 ;; esac
case "$ssh_user" in *[!A-Za-z0-9_.-]*|'') exit 2 ;; esac
case "$unit_hash" in *[!0-9a-f]*|'') exit 2 ;; esac
[ "${#unit_hash}" -eq 64 ] || exit 2
[ "$(id -u)" -eq 0 ] || exit 3
marker=/etc/hostops-vps-agent.managed
unit=/etc/systemd/system/hostops-vps-agent.service
binary=/usr/local/libexec/hostops-vps-agent
if [ ! -f "$marker" ] || [ -L "$marker" ]; then exit 4; fi
IFS=' ' read -r marker_version marker_host marker_user marker_unit_hash marker_binary_hash marker_extra < "$marker"
[ "$marker_version" = "hostops-managed-v2" ] && [ "$marker_host" = "$host_id" ] && [ "$marker_user" = "$ssh_user" ] && [ "$marker_unit_hash" = "$unit_hash" ] && [ -n "$marker_binary_hash" ] && [ -z "$marker_extra" ] || exit 4
case "$marker_binary_hash" in *[!0-9a-f]*|'') exit 4 ;; esac
[ "${#marker_binary_hash}" -eq 64 ] || exit 4
service_uid=$(id -u hostops 2>/dev/null) || exit 6
for parent in /etc/systemd/system /usr/local/libexec /var/lib /run; do
  [ -d "$parent" ] && [ ! -L "$parent" ] || exit 6
  parent_owner=$(stat -c '%u' "$parent")
  parent_mode=$(stat -c '%a' "$parent")
  [ "$parent_owner" = 0 ] && [ "$((0$parent_mode & 0022))" -eq 0 ] || exit 6
done
for dir in /var/lib/hostops /run/hostops; do
  if [ -e "$dir" ] || [ -L "$dir" ]; then
    [ -d "$dir" ] && [ ! -L "$dir" ] || exit 6
    dir_owner=$(stat -c '%u' "$dir")
    dir_mode=$(stat -c '%a' "$dir")
    [ "$dir_owner" = "$service_uid" ] && [ "$((0$dir_mode & 0022))" -eq 0 ] || exit 6
  fi
done
if [ -e "$unit" ] || [ -L "$unit" ]; then
  [ -f "$unit" ] && [ ! -L "$unit" ] || exit 5
  printf '%s  %s\n' "$unit_hash" "$unit" | sha256sum -c - >/dev/null || exit 5
fi
if [ -e "$binary" ] || [ -L "$binary" ]; then
  [ -f "$binary" ] && [ ! -L "$binary" ] || exit 5
  printf '%s  %s\n' "$marker_binary_hash" "$binary" | sha256sum -c - >/dev/null || exit 5
fi
systemctl disable --now hostops-vps-agent.service >/dev/null 2>&1
rm -f -- "$unit" "$binary" "$marker"
rm -f -- /var/lib/hostops/history.json /var/lib/hostops/.hostops-history-*.tmp
rmdir /var/lib/hostops 2>/dev/null || true
rm -f -- /run/hostops/agent.sock /run/hostops/agent.sock.lock
rmdir /run/hostops 2>/dev/null || true
systemctl daemon-reload
gpasswd -d "$ssh_user" hostops-readers >/dev/null 2>&1 || true
userdel hostops >/dev/null 2>&1 || true
groupdel hostops-readers >/dev/null 2>&1 || true
printf 'HostOps collector and its retained VPS history were removed. Unrecognized files in its state directories were preserved.\n'

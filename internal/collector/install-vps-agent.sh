#!/bin/sh
set -eu
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

src=$1
expected_hash=$2
expected_size=$3
host_id=$4
ssh_user=$5
unit_content=$6
unit_hash=$7

case "$expected_hash" in
  *[!0-9a-f]*|'') exit 2 ;;
esac
[ "${#expected_hash}" -eq 64 ] || exit 2
case "$unit_hash" in *[!0-9a-f]*|'') exit 2 ;; esac
[ "${#unit_hash}" -eq 64 ] || exit 2
case "$expected_size" in *[!0-9]*|'') exit 2 ;; esac
[ "$expected_size" -ge 100000 ] && [ "$expected_size" -le 50000000 ] || exit 2
case "$host_id" in [A-Za-z0-9]* ) : ;; *) exit 2 ;; esac
case "$host_id" in *[!A-Za-z0-9_.-]*|'') exit 2 ;; esac
case "$ssh_user" in [A-Za-z_]* ) : ;; *) exit 2 ;; esac
case "$ssh_user" in *[!A-Za-z0-9_.-]*|'') exit 2 ;; esac

[ "$(id -u)" -eq 0 ] || exit 3
[ -d /run/systemd/system ] && command -v systemctl >/dev/null 2>&1 || exit 4
id "$ssh_user" >/dev/null 2>&1 || exit 5

unit=/etc/systemd/system/hostops-vps-agent.service
marker=/etc/hostops-vps-agent.managed
binary=/usr/local/libexec/hostops-vps-agent
state=/var/lib/hostops
runtime=/run/hostops
for parent in /etc/systemd/system /usr/local /var/lib /run; do
  [ -d "$parent" ] && [ ! -L "$parent" ] || exit 6
  parent_owner=$(stat -c '%u' "$parent")
  parent_mode=$(stat -c '%a' "$parent")
  [ "$parent_owner" = 0 ] && [ "$((0$parent_mode & 0022))" -eq 0 ] || exit 6
done
if [ -e "$unit" ] || [ -L "$unit" ] || [ -e "$marker" ] || [ -L "$marker" ] || [ -e "$binary" ] || [ -L "$binary" ] || [ -e "$state" ] || [ -L "$state" ] || [ -e "$runtime" ] || [ -L "$runtime" ] || getent passwd hostops >/dev/null 2>&1 || getent group hostops-readers >/dev/null 2>&1; then
  exit 6
fi
[ -f "$src" ] && [ ! -L "$src" ] || exit 7

created_group=0
created_user=0
added_group=0
created_binary=0
created_state=0
created_unit=0
created_marker=0
installed=0
temporary=
rollback() {
  status=$?
  trap - EXIT HUP INT TERM
  if [ "$installed" -ne 1 ]; then
    systemctl disable --now hostops-vps-agent.service >/dev/null 2>&1 || true
    [ -z "$temporary" ] || rm -f -- "$temporary"
    [ "$created_unit" -ne 1 ] || rm -f -- "$unit"
    [ "$created_marker" -ne 1 ] || rm -f -- "$marker"
    [ "$created_binary" -ne 1 ] || rm -f -- "$binary"
    [ "$created_state" -ne 1 ] || rm -rf -- "$state"
    if [ "$added_group" -eq 1 ]; then gpasswd -d "$ssh_user" hostops-readers >/dev/null 2>&1 || true; fi
    if [ "$created_user" -eq 1 ]; then userdel hostops >/dev/null 2>&1 || true; fi
    if [ "$created_group" -eq 1 ]; then groupdel hostops-readers >/dev/null 2>&1 || true; fi
    systemctl daemon-reload >/dev/null 2>&1 || true
  fi
  exit "$status"
}
trap rollback EXIT HUP INT TERM

groupadd --system hostops-readers
created_group=1
useradd --system --no-create-home --home-dir /var/lib/hostops --shell /usr/sbin/nologin --gid hostops-readers hostops
created_user=1
if [ "$ssh_user" != "root" ]; then
  gpasswd -a "$ssh_user" hostops-readers >/dev/null
  added_group=1
fi

if [ -L /usr/local/libexec ] || { [ -e /usr/local/libexec ] && [ ! -d /usr/local/libexec ]; }; then exit 6; fi
install -d -o root -g root -m 0755 /usr/local/libexec
temporary=$(mktemp /usr/local/libexec/.hostops-agent.XXXXXX)
timeout 30s head -c "$expected_size" "$src" > "$temporary"
[ "$(wc -c < "$temporary" | tr -d ' ')" = "$expected_size" ] || exit 8
printf '%s  %s\n' "$expected_hash" "$temporary" | sha256sum -c - >/dev/null
chmod 0755 "$temporary"
chown root:root "$temporary"
created_binary=1
mv -f -- "$temporary" "$binary"
temporary=

created_state=1
install -d -o hostops -g hostops-readers -m 2750 "$state"
created_unit=1
printf '%s' "$unit_content" > "$unit"
printf '%s  %s\n' "$unit_hash" "$unit" | sha256sum -c - >/dev/null
chmod 0644 "$unit"
chown root:root "$unit"
created_marker=1
printf 'hostops-managed-v2 %s %s %s %s\n' "$host_id" "$ssh_user" "$unit_hash" "$expected_hash" > "$marker"
chmod 0644 "$marker"
chown root:root "$marker"
systemctl daemon-reload
systemctl enable --now hostops-vps-agent.service >/dev/null
systemctl is-active --quiet hostops-vps-agent.service
installed=1
trap - EXIT HUP INT TERM
if [ "$ssh_user" = "root" ]; then
  printf 'HostOps collector installed and active.\n'
else
  printf 'HostOps collector installed and active. Reconnect SSH once to apply the read-only group membership.\n'
fi

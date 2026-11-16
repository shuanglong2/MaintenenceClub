#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行" >&2
  exit 1
fi
if [ "$#" -ne 1 ]; then
  echo "用法: deploy.sh <版本标签 | 本地二进制>" >&2
  exit 1
fi

root=/srv/maintclub
arg="$1"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [ -f "$arg" ]; then
  ver="local-$(date -u +%Y%m%d%H%M%S)"
  install -m 0755 "$arg" "$tmp/mclub"
else
  case "$arg" in
    *[/\\]*|*..*)
      echo "版本标签无效" >&2
      exit 1
      ;;
  esac
  ver="$arg"
  base="https://github.com/shuanglong2/MaintenenceClub/releases/download/${ver}"
  curl -fL --retry 3 -o "$tmp/mclub-linux-x64" "${base}/mclub-linux-x64"
  curl -fL --retry 3 -o "$tmp/SHA256SUMS" "${base}/SHA256SUMS"
  (
    cd "$tmp"
    grep ' mclub-linux-x64$' SHA256SUMS > check.txt
    sha256sum -c check.txt
  )
  install -m 0755 "$tmp/mclub-linux-x64" "$tmp/mclub"
fi

dest="${root}/releases/${ver}"
if [ -e "$dest" ]; then
  echo "版本已存在: ${dest}" >&2
  exit 1
fi
install -d -o mclub -g mclub -m 0755 "$dest"
install -m 0755 -o mclub -g mclub "$tmp/mclub" "$dest/mclub"

mkdir -p "${root}/backups/db"
if ! runuser -u mclub -- pg_dump -Fc -d mclub -f "${root}/backups/db/pre-${ver}.dump"; then
  echo "数据库备份失败, 继续发布" >&2
fi

runuser -u mclub -- bash -c 'set -a; . /etc/mclub/mclub.env; set +a; exec "$1" migrate' -- "$dest/mclub"

prev=""
if [ -L "${root}/app" ]; then
  prev="$(readlink -f "${root}/app")"
fi
ln -sfn "$dest" "${root}/app"
systemctl restart mclub

ok=0
for _ in $(seq 1 25); do
  if curl -fsS http://127.0.0.1:8080/healthz | grep -q '"database":"ok"'; then
    ok=1
    break
  fi
  sleep 0.2
done
if [ "$ok" != 1 ]; then
  echo "健康检查失败, 回滚" >&2
  journalctl -u mclub -n 40 --no-pager || true
  if [ -n "$prev" ]; then
    ln -sfn "$prev" "${root}/app"
    systemctl restart mclub
  fi
  exit 1
fi
if [ -n "$prev" ]; then
  printf '%s\n' "$prev" > "${root}/previous"
fi
echo "已发布 ${ver}"

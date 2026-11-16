#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行" >&2
  exit 1
fi

src="$(cd "$(dirname "$0")" && pwd)"

apt-get update
apt-get install -y postgresql-18 nginx curl

if ! id mclub >/dev/null 2>&1; then
  useradd --system --home-dir /srv/maintclub --shell /usr/sbin/nologin mclub
fi

install -d -o mclub -g mclub -m 0750 \
  /srv/maintclub \
  /srv/maintclub/releases \
  /srv/maintclub/uploads \
  /srv/maintclub/backups \
  /srv/maintclub/backups/db

install -d -m 0755 /etc/mclub
if [ ! -f /etc/mclub/mclub.env ]; then
  install -m 0640 -o root -g mclub "$src/mclub.env.example" /etc/mclub/mclub.env
fi

if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_roles WHERE rolname='mclub'" | grep -q 1; then
  runuser -u postgres -- createuser --no-superuser --no-createdb --no-createrole mclub
fi
if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='mclub'" | grep -q 1; then
  runuser -u postgres -- createdb --owner=mclub mclub
fi
if ! runuser -u postgres -- psql -tAc "SELECT 1 FROM pg_database WHERE datname='mclub_dev'" | grep -q 1; then
  runuser -u postgres -- createdb --owner=mclub mclub_dev
fi

pgconf="$(find /etc/postgresql -type d -path '*/main/conf.d' | sort | tail -n 1)"
if [ -z "$pgconf" ]; then
  echo "找不到 PostgreSQL conf.d" >&2
  exit 1
fi
install -m 0644 "$src/postgresql-mclub.conf" "$pgconf/90-mclub.conf"
systemctl restart postgresql

install -m 0644 "$src/mclub.service" /etc/systemd/system/mclub.service
install -m 0755 "$src/backup.sh" /usr/local/sbin/mclub-backup
install -m 0644 "$src/mclub-backup.service" /etc/systemd/system/mclub-backup.service
install -m 0644 "$src/mclub-backup.timer" /etc/systemd/system/mclub-backup.timer
systemctl daemon-reload
systemctl enable mclub
systemctl enable --now mclub-backup.timer

install -d -m 0755 /etc/nginx/snippets
install -m 0644 "$src/mclub-proxy.conf" /etc/nginx/snippets/mclub-proxy.conf
install -m 0644 "$src/mclub-limit.conf" /etc/nginx/conf.d/mclub-limit.conf
install -m 0644 "$src/mclub.nginx" /etc/nginx/sites-available/mclub
ln -sfn /etc/nginx/sites-available/mclub /etc/nginx/sites-enabled/mclub
rm -f /etc/nginx/sites-enabled/default
nginx -t
systemctl reload nginx

echo "安装完成. 接下来用 deploy.sh 发布二进制."
echo "请再确认: 未启用 amd64v3, 已设置交换分区, SSH 只允许密钥, ufw 只开放 22 和 80, 时间同步已开启."

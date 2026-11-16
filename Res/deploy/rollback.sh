#!/usr/bin/env bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
  echo "请用 root 运行" >&2
  exit 1
fi

root=/srv/maintclub
if [ ! -f "${root}/previous" ]; then
  echo "没有上一版本" >&2
  exit 1
fi
prev="$(cat "${root}/previous")"
if [ ! -x "${prev}/mclub" ]; then
  echo "上一版本不可执行: ${prev}" >&2
  exit 1
fi
ln -sfn "$prev" "${root}/app"
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
  echo "回滚后健康检查失败" >&2
  exit 1
fi
echo "已回到 ${prev}"

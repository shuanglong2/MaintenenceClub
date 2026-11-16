#!/usr/bin/env bash
set -euo pipefail

dir=/srv/maintclub/backups/db
mkdir -p "$dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="${dir}/mclub-${stamp}.dump"

if ! runuser -u mclub -- pg_dump -Fc -d mclub -f "$out"; then
  echo "数据库备份失败" >&2
  touch "${dir}/LAST_FAILED"
  exit 1
fi

rm -f "${dir}/LAST_FAILED"
date -u +%Y-%m-%dT%H:%M:%SZ > "${dir}/LAST_OK"
find "$dir" -maxdepth 1 -type f -name 'mclub-*.dump' -printf '%T@ %p\n' \
  | sort -nr \
  | awk 'NR>7 {print $2}' \
  | while read -r old; do
      rm -f "$old"
    done
echo "已备份 ${out}"

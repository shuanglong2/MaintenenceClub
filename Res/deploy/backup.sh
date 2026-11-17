#!/usr/bin/env bash
set -euo pipefail

root=/srv/maintclub
dbdir=$root/backups/db
updir=$root/backups/uploads
uploads=$root/uploads
status=$root/backups/LAST_STATUS
keep=7
# 备份目录超过 8 GB 时删掉最旧的上传快照, 至少留 1 份. 数据库转储仍保留 7 份.
budget=$((8 * 1024 * 1024 * 1024))

fail() {
  echo "$1" >&2
  mkdir -p "$root/backups"
  now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'result=fail\nat=%s\nnote=%s\n' "$now" "$1" > "$status"
  chmod 644 "$status"
  exit 1
}

if ! command -v rsync >/dev/null 2>&1; then
  fail "未安装 rsync"
fi

mkdir -p "$dbdir" "$updir" "$uploads"
chown mclub:mclub "$dbdir" "$updir"
chmod 750 "$dbdir" "$updir"

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
out="${dbdir}/mclub-${stamp}.dump"
if ! runuser -u mclub -- pg_dump -Fc -d mclub -f "$out"; then
  rm -f "$out"
  fail "数据库备份失败"
fi
chown mclub:mclub "$out"
chmod 640 "$out"

find "$dbdir" -maxdepth 1 -type f -name 'mclub-*.dump' -printf '%T@ %p\n' \
  | sort -nr \
  | awk 'NR>7 {print $2}' \
  | while read -r old; do
      rm -f "$old"
    done

dest="${updir}/${stamp}"
prev="$(find "$updir" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -n | awk 'END {print $2}')"
mkdir -p "$dest"
chown mclub:mclub "$dest"
if [ -n "${prev}" ] && [ -d "${prev}" ]; then
  if ! runuser -u mclub -- rsync -a --delete --link-dest="${prev}" "${uploads}/" "${dest}/"; then
    rm -rf "$dest"
    fail "上传目录备份失败"
  fi
elif ! runuser -u mclub -- rsync -a --delete "${uploads}/" "${dest}/"; then
  rm -rf "$dest"
  fail "上传目录备份失败"
fi

find "$updir" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' \
  | sort -nr \
  | awk -v keep="$keep" 'NR>keep {print $2}' \
  | while read -r old; do
      case "$old" in
        "$updir"/*) rm -rf "$old" ;;
      esac
    done

while true; do
  used="$(du -sb "$root/backups" | awk '{print $1}')"
  count="$(find "$updir" -mindepth 1 -maxdepth 1 -type d | wc -l)"
  if [ "$used" -le "$budget" ] || [ "$count" -le 1 ]; then
    break
  fi
  oldest="$(find "$updir" -mindepth 1 -maxdepth 1 -type d -printf '%T@ %p\n' | sort -n | awk 'NR==1 {print $2}')"
  case "$oldest" in
    "$updir"/*) rm -rf "$oldest" ;;
    *) break ;;
  esac
done

now="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
printf 'result=ok\nat=%s\ndb=%s\nuploads=%s\n' "$now" "$out" "$dest" > "$status"
chmod 644 "$status"
rm -f "${dbdir}/LAST_FAILED"
printf '%s\n' "$now" > "${dbdir}/LAST_OK"
chmod 644 "${dbdir}/LAST_OK"
echo "已备份 ${out}"
echo "已快照 ${dest}"

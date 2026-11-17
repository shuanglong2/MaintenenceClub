#!/usr/bin/env bash
# 在另一台电脑上执行, 把服务器上的备份拉下来. 不要在服务器上往外推.
# 用法: pull-backup.sh 用户@服务器 本地目录
# 该用户要能读取 /srv/maintclub/backups (加入 mclub 组). 库转储含密码哈希, 不要放进 git 仓库.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "用法: pull-backup.sh 用户@服务器 本地目录" >&2
  exit 1
fi

remote=$1
dest=$2
mkdir -p "${dest}/db" "${dest}/uploads"
rsync -a --delete "${remote}:/srv/maintclub/backups/db/" "${dest}/db/"
rsync -a "${remote}:/srv/maintclub/backups/uploads/" "${dest}/uploads/"
echo "已拉到 ${dest}"

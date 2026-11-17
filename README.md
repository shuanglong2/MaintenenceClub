# 维修组

学校维修组的内网网站. 程序名是 mclub, 用 Go 编写, 仓库名是 MaintenenceClub.

网站只在内网使用, 继续走 HTTP, 不启用 HTTPS. 手机不需要安装内部证书. 二维码用手机自带相机打开物品页, 页面里不调用相机. 同一局域网里的人有机会看到未加密的流量, 改数据仍要带登录后的校验令牌. 以后若要改成 HTTPS, 由 Nginx 终止 TLS, 再把 `MCLUB_COOKIE_SECURE` 设为 `true`.

不在本机编译. 推送到 GitHub 后, Actions 检查格式, 测试, 并交叉编译 linux-x64 与 win-x64. 服务器运行 `Publish/linux-x64/mclub`, 前面是 Nginx, 数据库是 PostgreSQL 18.

登录后可以写文档, 登记库存, 也可以安排活动. 正文是 Markdown, 图片和附件从编辑页上传. 仅管理层的页面, 组员打不开, 搜索也找不到. 库存数量由流水加总, 借还只改变器材状态. 活动时间按北京时间填写, 全天活动只记日期. 任务可以列表或看板查看, 也能从活动页直接建出来. 登录后的首页按人汇总未完成任务, 即将到期, 近期活动, 低库存和最近文档. 维修记录可以领用元件, 作废后库存加回, 器材页能看到维修保养历史. 财务流水按分记账, 手头和储蓄由加总得到, 组员看不到.

只改 `Docs` 的提交不会触发编译. 编译固定 `GOAMD64=v1`, `CGO_ENABLED=0`, 以便在没有 AVX 的 J1800 上运行. Linux 构建不额外剥离调试信息, 两个平台都不使用 UPX.

## 目录

| 路径 | 内容 |
| --- | --- |
| `Src` | Go 源码, 入口是 `Src/cmd/mclub` |
| `Docs` | 计划与技术文档 |
| `Res` | 资源文件 |
| `Publish/linux-x64` | Linux x64 二进制 |
| `Publish/win-x64` | Windows x64 二进制 |

二进制在 Actions 的 artifact `Publish` 里, 不提交进 git. 增加依赖后, 若流水线因 `go.sum` 失败, 下载 artifact `go-mod` 再提交.

## 命令

```text
mclub serve
mclub migrate
mclub version
mclub admin create <登录名>
mclub admin reset-password <登录名>
```

`serve` 默认监听 `127.0.0.1:8080`. `migrate` 把数据库补到最新, 重复执行是安全的. `admin create` 只在还没有人员时创建第一名组长, 密码从终端读取. 环境变量见 `Res/deploy/mclub.env.example`.

## 部署

服务器上的安装, 发布和回滚脚本在 `Res/deploy`. 先在 J1800 上执行 `install.sh`, 再用 `deploy.sh <版本标签>` 或 `deploy.sh <本地二进制>` 发布. 发布前会备份数据库, 健康检查失败会切回上一版. 每天 03:00 (Asia/Shanghai) 由 `mclub-backup.timer` 备份数据库和上传目录, 各保留 7 份. 组长登录后打开 `/system` 查看磁盘, 备份结果和版本.

## 备份与恢复

数据库转储在 `/srv/maintclub/backups/db`, 上传目录的硬链接快照在 `/srv/maintclub/backups/uploads`. 备份总占用超过 8 GB 时删掉较旧的快照, 至少留 1 份. 结果写在 `/srv/maintclub/backups/LAST_STATUS`.

异机副本在另一台电脑上执行:

```text
Res/deploy/pull-backup.sh 用户@服务器 本地目录
```

拉取账号要能读取备份目录, 把该系统用户加入 `mclub` 组即可. 不要把备份放进 git 仓库.

恢复演练使用临时库, 不要覆盖正在使用的 `mclub`:

1. 记下开始时间.
2. `runuser -u postgres -- createdb --owner=mclub 临时库名`
3. `runuser -u mclub -- pg_restore --no-owner --no-acl -d 临时库名 /srv/maintclub/backups/db/最新.dump`
4. 对比 `people.people`, `docs.pages`, `inventory.items` 的行数, 并抽几条文档标题.
5. `rsync -a /srv/maintclub/backups/uploads/最新/ /tmp/mclub-restore-uploads/` 后对比文件数.
6. 记下结束时间. 临时库可以留着. 不要删除 `mclub` 和已有的 `mclub_restore_test`.

2026-11-17 在同一台服务器上按上面的步骤还原到临时库 `mclub_restore_m7`, 从建库到对比完成用了 2 秒. 人员, 文档, 物品, 活动和财务的行数都是 1, 4, 2, 1, 1, 四篇文档标题一致. 当时上传目录没有文件, 快照里的文件数也是 0. 没有第二台机器, 所以上传文件没有在另一台机器上展开. 旧的 `mclub_restore_test` 没有删除.

## 安装, 升级与回滚

在服务器上用 root 执行 `Res/deploy/install.sh`. 它用系统自带的 PostgreSQL 18 和 Nginx, 建立 `mclub` 用户, 数据库和每天 03:00 的备份定时器. 环境变量在 `/etc/mclub/mclub.env`.

升级时把新的 Linux 二进制交给 `deploy.sh`. 它会先转储数据库, 再执行 `migrate`, 然后切换 `/srv/maintclub/app` 并重启. 迁移只向前, 不会自动撤销. 健康检查失败时, 脚本会把链接指回上一版.

手动回滚:

```text
ln -sfn "$(cat /srv/maintclub/previous)" /srv/maintclub/app
systemctl restart mclub
```

数据库结构如果已经升过, 只换回旧二进制可能对不上. 那种情况用备份里的转储恢复到临时库核对, 不要直接覆盖正在使用的 `mclub`.

## 常见故障

打不开页面时先看 `curl -fsS http://127.0.0.1:8080/healthz`. `database` 不是 `ok` 就看 `journalctl -u mclub -n 80`. 备份失败看 `/srv/maintclub/backups/LAST_STATUS` 和 `journalctl -u mclub-backup`. 组长的状态页会在磁盘剩余低于 15%, 或上传占用达到 6 GB 配额的 80% 时给出提示. 单个上传不能超过 20 MB. 登录后如果一直停在改密码页, 是因为初始密码还没改过.

打 `v*` 标签后, Actions 把 `mclub-linux-x64`, `mclub-win-x64.exe` 和 `SHA256SUMS` 发到 GitHub Release.

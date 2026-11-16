# 维修组

学校维修组的内网网站. 程序名是 mclub, 用 Go 编写, 仓库名是 MaintenenceClub.

网站只在内网使用. 不在本机编译. 推送到 GitHub 后, Actions 检查格式, 测试, 并交叉编译 linux-x64 与 win-x64. 服务器运行 `Publish/linux-x64/mclub`, 前面是 Nginx, 数据库是 PostgreSQL 18.

登录后可以写文档, 登记库存, 也可以安排活动. 正文是 Markdown, 图片和附件从编辑页上传. 仅管理层的页面, 组员打不开, 搜索也找不到. 库存数量由流水加总, 借还只改变器材状态. 活动时间按北京时间填写, 全天活动只记日期. 任务可以列表或看板查看, 也能从活动页直接建出来.

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

服务器上的安装, 发布和回滚脚本在 `Res/deploy`. 先在 J1800 上执行 `install.sh`, 再用 `deploy.sh <版本标签>` 或 `deploy.sh <本地二进制>` 发布. 发布前会备份数据库, 健康检查失败会切回上一版. 每天 03:00 (Asia/Shanghai) 由 `mclub-backup.timer` 再备份一次, 保留 7 份.

打 `v*` 标签后, Actions 把 `mclub-linux-x64`, `mclub-win-x64.exe` 和 `SHA256SUMS` 发到 GitHub Release.

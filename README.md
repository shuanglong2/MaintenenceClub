# 维修组

学校维修组的内网网站. 程序名是 mclub, 用 Go 编写, 仓库名是 MaintenenceClub.

网站只在内网使用. 不在本机编译. 推送到 GitHub 后, Actions 检查格式, 测试, 并交叉编译 linux-x64 与 win-x64. 服务器运行 `Publish/linux-x64/mclub`, 前面是 Nginx, 数据库是 PostgreSQL 18.

只改 `Docs` 的提交不会触发编译. 编译固定 `GOAMD64=v1`, `CGO_ENABLED=0`, 以便在没有 AVX 的 J1800 上运行. Linux 二进制去掉调试信息, 两个平台都会再用 UPX 的 LZMA 压缩.

## 目录

| 路径 | 内容 |
| --- | --- |
| `Src` | Go 源码, 入口是 `Src/cmd/mclub` |
| `Docs` | 计划与技术文档 |
| `Res` | 资源文件 |
| `Publish/linux-x64` | Linux x64 二进制 |
| `Publish/win-x64` | Windows x64 二进制 |

二进制在 Actions 的 artifact `Publish` 里, 不提交进 git. 增加依赖后, 若流水线因 `go.sum` 失败, 下载 artifact `go-mod` 再提交.

## 运行

服务器把二进制放到 `/srv/maintclub`, 由 systemd 启动. 默认监听 `127.0.0.1:8080`, 可用环境变量 `MCLUB_ADDR` 修改. Nginx 反向代理到这个地址.

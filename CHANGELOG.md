# Changelog

## Unreleased

- 新增 `Docs/Roadmap.md`: 在核实 Ubuntu 26.04, PostgreSQL 18, Go 标准库 CSRF, UPX, 中文检索等前提后, 把 Plan1 展开为 M0 到 M8 的任务清单, 并列出对 Plan1 的修订建议.
- M0 骨架: `serve`, `migrate`, `version`, 配置, 迁移器, 页面和健康检查. CI 增加 PostgreSQL 18, vet, race, staticcheck, 冒烟和 UPX 开关. 标签发布与每周漏洞检查, 以及 `Res/deploy` 安装脚本.

## 0.1.0 - 2026-11-16

- 用 Go 重新初始化 MClub, 源码放在 `Src`, 计划放在 `Docs`.
- 由 GitHub Actions 交叉编译 linux-x64 与 win-x64, 并在成功后清理过期缓存.
- Linux 编译去掉符号表, DWARF 和 build id. 两个平台的二进制再用 UPX LZMA 压缩.

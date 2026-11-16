# Changelog

## 0.1.0 - 2026-11-16

- 用 Go 重新初始化 MClub, 源码放在 `Src`, 计划放在 `Docs`.
- 由 GitHub Actions 交叉编译 linux-x64 与 win-x64, 并在成功后清理过期缓存.
- Linux 编译去掉符号表, DWARF 和 build id. 两个平台的二进制再用 UPX LZMA 压缩.

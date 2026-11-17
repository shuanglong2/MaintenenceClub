# 维修组内部服务器: 开发路线图 (Roadmap)

本文档由 `Plan1.md` 展开而来, 目的是让一个人在没有本地编译环境的条件下, 按固定顺序把系统做完. 需求以 `Plan1.md` 为准, 本文只补充 "怎么做, 先做什么, 怎么算做完". 与 `Plan1.md` 不一致的地方集中列在第 2 节, 确认后再回写 `Plan1.md`.

调研日期: 2026-11-16. 结论所依据的来源见第 12 节.

## 0. 阅读说明

- 任务编号形如 `M2-05`. 复选框 `- [ ]` 随开发勾选.
- 体量: S 不超过 1 天, M 2 到 4 天, L 约 1 到 2 周 (按 "一个人加 AI, 每晚几小时" 估算, 仅用于排序, 不是承诺).
- 每个里程碑结束都要做三件事: 打标签 `v0.N.0`, 部署到 J1800, 在第 11 节填一行实测数据.
- 通用完成标准 (DoD), 每个任务都适用:
  1. CI 全绿 (格式, vet, 测试, 编译, 冒烟)
  2. 数据库变更只增不改, 空库能一路迁移到最新
  3. 业务规则有单元测试, 权限变更同步更新权限矩阵测试
  4. 写操作有审计记录, 页面在 360 px 宽度下可用, 界面全中文
  5. 在 J1800 上手动走过一遍验收步骤, 并确认内存没有明显增长

## 1. 结论摘要

1. 技术栈 `Plan1.md` 选得对, 无需推翻. 已核实: Ubuntu 26.04 LTS 自带 PostgreSQL 18 (18.6), amd64 基线不要求 AVX; 唯一的坑是不能开启 `amd64v3` 架构变体.
2. 最大的工程风险不是技术, 而是 **没有本地编译 = 每次反馈要等 CI**. 所以路线图第一步 (M0) 先把 "快速反馈" 做出来: CI 里带真实 PG 18 的集成测试, 冒烟测试, 以及页面模板可以从磁盘热读取的开发模式.
3. 备份必须 **提前到 M1**, 并且必须有 **异机副本**. 只有一块 32 GB 盘, 备份放同一块盘等于没有备份.
4. 中文搜索不装扩展, 用 "应用侧二元分词 + `simple` 全文索引". 规模只有几百到几千页, 够用, 以后可换.
5. 局域网 HTTP 下浏览器不发送 `Sec-Fetch-Site`, 标准库 CSRF 防护会退化为只比较 `Origin` 与 `Host`. 因此加一层会话令牌, 并要求 Nginx 原样传递 `Host`.
6. UPX 按要求保留, 但它会让代码页变成不可回收的匿名内存, 并增加启动时间. 在 M0 用 J1800 实测, CI 保留一个开关, 数据说了算.
7. 里程碑顺序基本沿用 Plan1, 做三处调整: 备份前移, 文件存储提前到 M2 (文档第一次需要图片), 任务与活动拆成 M4a/M4b/M4c.

## 2. 对 Plan1 的修订建议

| # | Plan1 原文 | 建议 | 原因 |
|---|---|---|---|
| 1 | 前端脚本放 `Res` | 模板与静态文件放 `Src/internal/web/` 并用 `go:embed`; `Res` 只放部署脚本, 配置模板, 图片源文件 | `go:embed` 不能引用包目录之外 (不能含 `..`), `Res` 在仓库根, 嵌入不了; 而规则又要求 Go 源码只能在 `Src` |
| 2 | 备份写在 M7 | 每日 `pg_dump` 放进 M1, 异机副本与恢复演练留在 M7 | 一有真实账号就有数据, 不能裸奔到 M7 |
| 3 | 备份到备份目录 | 每周至少一次复制到另一台设备 (组长电脑或 U 盘) | 同盘备份挡不住硬盘损坏 |
| 4 | 服务器 "下载 linux-x64 二进制" | 打标签时由 CI 发布 GitHub Release (含 sha256), 服务器下载 Release 资产; 访问不了 GitHub 时用 `scp` | 同一仓库的 Actions artifact 下载 API 需要令牌, Release 资产公开可直接下载 |
| 5 | "防跨站请求伪造" | `http.CrossOriginProtection` 加会话令牌 | 见决策 D5 |
| 6 | "中文先按字符分词" | 具体为二元分词, 见决策 D8 | 默认解析器不会把连续汉字切开 |
| 7 | CI 6 步 | 增加 `go vet`, PG 18 服务容器, `-race` 测试, 冒烟, Release, Dependabot | 见第 4.2 节 |
| 8 | 无 | 人员与库存提供 CSV 导入 | 现有器材清单大概率在 Excel 里 |
| 9 | 账号 "组长创建" | 增加命令行子命令: 创建首个组长, 重置组长密码 | 不开放注册时, 组长忘记密码必须有服务器侧的出口 |
| 10 | 角色三选一 | 增加不变量: 在组的组长不得少于 1 人 | 换届时先提拔再降级, 避免系统里没有管理员 |

## 3. 技术决策与依据

### D1 目录: 嵌入资源放在 `Src`

```text
Src/cmd/mclub/main.go            子命令: serve, migrate, admin, version
Src/internal/web/                布局模板, 静态文件 (htmx, css, js), embed.FS
Src/internal/shared/             config, db, migrate, auth, authz, audit, files, mdown, search 等通用能力
Src/internal/<模块>/             people tasks events logs inventory finance docs
Res/deploy/                      systemd, nginx, postgresql 配置, 安装, 部署, 备份, 恢复脚本
Res/img/                         图标与 logo 源文件
```

模块目录模板 (所有模块一致, 方便让 AI 逐模块开发):

```text
<模块>/
  model.go        类型与枚举
  store.go        SQL (只访问自己的 schema)
  service.go      业务规则, 事务边界, 权限检查
  handler.go      HTTP 处理, 表单解析
  routes.go       路由注册, 首页组件注册
  templates/      本模块的 html
  migrations/     <模块>_<序号>_<说明>.sql
  *_test.go
```

跨模块规则:

- 只通过对方导出的 service 接口调用, 不写跨 schema 的 SQL; 允许外键指向 `people.people(id)` 等稳定表
- service 方法接收事务参数, 这样 "维修记录更换元件 → 库存领用" 能在同一个事务里完成
- 人员姓名走内存目录缓存 (30 人), 页面渲染时按 ID 取名, 不用联表; 历史记录同时存当时姓名快照

### D2 数据库访问

- 驱动 `github.com/jackc/pgx/v5`, 原生接口加 `pgxpool`, 纯 Go. `lib/pq` 已进入维护模式
- 连接池上限 6, 空闲 2. pgx 每个连接会保留缓冲区, 连接数过大会明显增加内存 (社区已有案例). PostgreSQL `max_connections` 设 20, 其中留给备份, 迁移, 手工维护
- 用 Unix 套接字加对等认证连接 (系统用户 `mclub` 对应数据库角色 `mclub`), 不需要数据库密码
- 手写 SQL, 不用 ORM. 也不用 `sqlc`: 它要先生成代码再提交, 而本机不能运行 Go 工具链, 会多出一条 CI 回写流程

### D3 迁移

- 自写最小迁移器 (约 100 行): 嵌入各模块的 `migrations/*.sql`, 表 `public.schema_migrations(module, version, applied_at)`, 用 `pg_advisory_lock` 防并发, 每个文件一个事务, 只前进不回退
- 回滚方式是 "升级前备份 + 恢复", 与 Plan1 "升级前先做一次备份" 一致
- 模块应用顺序在 `main` 里显式声明: people → shared 表 (audit, files) → docs → inventory → events → tasks → logs → finance
- 备选: `pressly/goose` (支持嵌入 SQL, 带回滚). 只有当自写迁移器出现维护负担时再换
- 部署顺序: 先 `mclub migrate`, 成功后才切换符号链接; 程序启动时发现库版本落后则拒绝启动并打印原因

### D4 PostgreSQL 18 用法

- 主键用 `bigint GENERATED ALWAYS AS IDENTITY`, 页面地址短 (`/tasks/123`). PG 18 已有 `uuidv7()`, 本系统用不上
- 金额 `bigint` (分), 时间 `timestamptz`, 日期 `date`. 显示时区固定 `time.FixedZone("CST", 8*3600)`, 中国没有夏令时, 不必嵌入 tzdata
- 生成列在 PG 18 默认是 virtual 且不能建索引, 需要索引的必须写 `STORED`
- 数量与金额的 "不可篡改" 在数据库层强制: 流水表加触发器, 禁止 `DELETE`, `UPDATE` 只允许把作废字段从空改为有值
- 审计表同样禁止 `UPDATE` 与 `DELETE`

### D5 CSRF 与会话

- 标准库 `http.CrossOriginProtection` (Go 1.25 起) 取代 `gorilla/csrf` (后者有 CVE-2025-47909)
- 局域网 `http://192.168.x.x` 不是 "可信来源", 浏览器不会发送 `Sec-Fetch-*` 头. 此时该中间件退化为比较 `Origin` 与 `Host`, 并且两头都没有时放行. 补救:
  1. 所有 `POST` 表单与 htmx 请求带会话绑定令牌 (隐藏字段或 `X-CSRF-Token`), 服务端校验
  2. Nginx 用 `proxy_set_header Host $http_host;` 保留原始主机与端口
  3. Cookie `HttpOnly`, `SameSite=Lax`, `Secure` 由配置控制 (HTTP 阶段为关)
  4. 所有改状态的操作只接受 `POST`, 绝不用 `GET`
- 会话存数据库表 `people.sessions` (只存令牌哈希). 每个请求按会话联表检查人员状态, 因此 "已退出" 立刻失效. 空闲 12 小时过期, 绝对 14 天过期, 改密码时撤销其他会话
- 登录限速: Nginx `limit_req` 加应用内按 `(登录名, IP)` 的指数退避. 不做账号锁定, 避免被人恶意锁号
- 密码: bcrypt, 代价参数在 J1800 上实测定到单次约 250 ms 以内; 限制同时进行的哈希数为 2; 密码最长 72 字节 (bcrypt 限制) 最短 8 位; 首次登录强制改密

### D6 HTTP 与 HTTPS

默认仍是内网 HTTP (Plan1 的默认值). 需要记住它带来的限制, 开发时不要依赖这些需要 "安全上下文" 的浏览器能力:

- 页面内相机扫码 (`getUserMedia`), 剪贴板 API, `crypto.randomUUID`, Service Worker 与 PWA
- 二维码标签方案因此只能是 "手机原生相机扫码打开链接", 不做页内扫码
- 若以后启用 HTTPS: Nginx 终止 TLS, 应用仅把 `MCLUB_COOKIE_SECURE` 改为开; 手机需要信任内部 CA. 决策放到 M8

### D7 Markdown

- 渲染: `github.com/yuin/goldmark` 带 GFM 扩展 (表格, 任务列表, 删除线), 默认 **不** 开 `WithUnsafe`
- 清洗: 渲染得到 HTML 后再用 `github.com/microcosm-cc/bluemonday` 的 `UGCPolicy`. 顺序必须是 "先渲染后清洗", 不能清洗 Markdown 源文
- 只对必须保留的标签放行 `class` (代码高亮若以后需要)
- 预览接口 `POST /md/preview` 与正式渲染走同一个函数, 所见即所得
- 测试: 一份 XSS 载荷语料 (script, onerror, `javascript:`, `data:`, iframe, svg) 走完整管线, 断言输出不含危险内容, 放进单元测试

### D8 中文搜索

候选方案核实结果:

| 方案 | 结论 |
|---|---|
| `pg_trgm` | 随 PostgreSQL 自带, 但按三字符切, 1 到 2 个汉字的查询用不上索引; 对汉字是否算 "字母" 取决于数据库区域设置, 需要实测 |
| `pg_bigm` | 二元切分, 适合 `LIKE`, 但需要额外安装包, 且 PG 18 扩展问题清单里仍有它的记录 |
| `zhparser` / `pg_jieba` | 分词质量好, 但需要额外安装与词典, 每次升级 PostgreSQL 主版本都要跟着编译或装包 |
| PGroonga | 需要额外软件源, 体积大 |

选择 **应用侧二元分词**, 不装任何扩展:

1. 保存页面时, Go 把标题与正文转成 "搜索词串": 连续汉字取相邻二元组 (`数据库` → `数据 据库`), 拉丁字母与数字按单词, 全部小写, 用空格分隔, 写入列 `search_tokens`
2. 列 `tsv` 为 `STORED` 生成列 `to_tsvector('simple', search_tokens)`, 建 GIN 索引
3. 查询串用同一个函数分词, 多个二元组用 `<->` 连成短语查询, 多个词之间 `&`
4. 查询只有 1 个汉字时, 退化为 `ILIKE` 顺序扫描 (数据量小, 毫秒级)
5. 结果摘要 (命中片段) 在 Go 里从原文取, 不用 `ts_headline`
6. 排序用 `ts_rank`

这个方案由 CI 里的 PG 18 容器做完整测试. 若试用后觉得召回率或排序不够, 再评估 `zhparser`, 到时再核对 Ubuntu 26.04 的软件包是否齐全. 这是 M2 的第一个任务 (先做 20 页样例对比), 不是预先写死.

### D9 文件与图片

- 目录 `uploads/<模块>/<年>/<月>/<随机名>`, 随机名 128 位随机数十六进制; 数据库记原文件名, 大小, 内容类型, 上传人, 校验和
- 先写临时文件, 再改名, 最后写数据库; 后台定期清理 "有文件无记录" 的孤儿
- 校验: 扩展名白名单, 用文件头判断真实类型 (`http.DetectContentType`), 请求体用 `http.MaxBytesReader` 限制大小
- 图片: 先 `image.DecodeConfig` 读文件头, 像素超过上限直接拒绝 (默认 48 MP); 否则解码, 应用 EXIF 方向 (Go 的 `image/jpeg` 不处理方向, 不转的话手机照片会横过来), 长边缩到 2000 px 内, 重新编码为 JPEG (有透明通道的 PNG 保持 PNG), 同时去掉 EXIF 里的定位信息; 缩放用 `golang.org/x/image/draw`
- 同一时刻只处理 1 张图片 (信号量). 一张 48 MP 的照片解码后可达几十到上百 MB, 在 2 GB 内存机器上不能并行
- 总容量上限由配置控制 (默认 6 GB). 达到 80% 时系统状态页告警, 达到 100% 拒绝上传
- 下载走应用授权后的接口 (`http.ServeContent`, 支持 Range), 附件一律 `Content-Disposition: attachment` 且 `X-Content-Type-Options: nosniff`; 图片内联显示

### D10 前端

- 以服务端渲染为主, 没有 JS 也必须能完成所有操作 (渐进增强)
- htmx 使用 2.0 系列 (当前 `latest` 为 2.0.11, 约 51 KB, 压缩后 16 KB), 固定版本, 文件放进仓库并在 CI 里核对 sha256. htmx 4.0 已发布但仍是 `next` 标签, 计划到 2027 年初再考虑, 不是本项目的必要升级
- 只在这些地方用 htmx: Markdown 预览, 看板卡片换列, 搜索联想, 借还与领用的行内提交, 局部刷新首页组件
- CSP: `default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'`, 禁止内联脚本与内联样式
- 月历, 看板, 统计图表全部服务端生成 HTML (统计用 CSS 条形图, 不引入图表库)
- 导出 CSV 加 UTF-8 BOM (Windows 中文版 Excel 才能正确打开), 并对以 `=`, `+`, `-`, `@` 开头的单元格前置单引号, 防公式注入

### D11 发布与分发

- 推送到 `main`: CI 编译并上传 `Publish` artifact (已实现)
- 推送标签 `v*`: 再创建 GitHub Release, 资产为 `mclub-linux-x64`, `mclub-win-x64.exe`, `SHA256SUMS`; 用预装的 `gh release create`, 令牌用 `GITHUB_TOKEN`, 不引入第三方 Action
- 服务器侧 `Res/deploy/deploy.sh <标签|本地文件>`: 校验 sha256 → 新建 `releases/<版本>` → 备份数据库 → `mclub migrate` → 切换符号链接 → 重启 → 检查 `/healthz` → 失败自动切回旧版本
- 若学校网络访问 GitHub 很慢或不通: 在你的电脑上下载 Release, 用 `scp` 传到服务器, 再执行同一个脚本 (脚本接受本地文件路径). 这条路径要在 M0 就演练一次
- 仓库是公开的: 绝不提交真实人员数据, 内网地址, 密码, 令牌. 服务器机密只放 `/etc/mclub/mclub.env` (权限 0600, 由 systemd `EnvironmentFile` 读取)

### D12 开发循环 (无本地编译)

每次改动的反馈路径是: 提交 → CI → 看结果. 为了不让它变成瓶颈:

1. 小步提交, 每次只改一个点. CI 有缓存, 目标是 3 分钟内出结果
2. 红灯立刻修, 不堆积
3. **开发模式** `MCLUB_DEV_ASSETS=<目录>`: 模板与静态文件改为从磁盘读取, 修改 HTML 与 CSS 后刷新浏览器即可, 不必重新编译 (发布版仍然用嵌入)
4. 在你的 Windows 电脑上直接运行 CI 产出的 `mclub.exe`, 数据库连接到服务器上的开发库 `mclub_dev` (通过 SSH 隧道; PostgreSQL 仍然只监听本机). 备选: 服务器上再起一个 8081 端口的开发实例
5. 子命令 `mclub seed-demo` 生成假数据 (人员, 器材, 页面, 流水), 仅在 `MCLUB_ENV=dev` 且库为空时允许执行
6. 依赖引入集中进行: 第一次写 `import` 后, CI 的 `go mod tidy` 会失败并产出 `go-mod` artifact, 下载提交一次即可. 每个里程碑开始时一次性把本阶段要用的依赖引入, 减少来回
7. 依赖升级交给 Dependabot (见 E8), 由 GitHub 自己生成包含 `go.sum` 的 PR, 不在本机生成

### D13 UPX

要求保留 (减少约三分之二体积). 已知代价: 代码页变成匿名内存, 不能被系统回收; 启动要解压, 在老 CPU 上可能多出几百毫秒; 部分安全软件会误报 Windows 压缩件. 本项目只有一个进程实例, 增加的内存约等于二进制体积. 做法:

- CI 增加变量 `UPX_ENABLE` (默认开). M0 在 J1800 上对比 "开/关" 两个版本的 RSS 与冷启动, 结果写入第 11 节
- 判定线: 空闲 RSS 增量不超过 20 MB 且冷启动不超过 2 秒, 否则 Linux 版改为不压缩, 只保留 `-s -w` 与 `-trimpath`
- 冒烟测试必须运行压缩后的 Linux 二进制 (E6)

### D14 运行参数

- `GOMEMLIMIT=120MiB` (软上限, 让 GC 在接近目标时更积极), `GOMAXPROCS=2`
- systemd: `User=mclub`, `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ReadWritePaths=/srv/maintclub/uploads /srv/maintclub/backups`, `PrivateTmp=yes`, `Restart=on-failure`, `MemoryMax=300M` (硬上限, 防止泄漏拖垮整机)
- 日志: `log/slog` 输出 JSON 到标准输出, 交给 journald; 不写日志文件. journald 限制总量 200 MB
- 优雅停机: 收到 `SIGTERM` 后停止接收新请求, 最长等待 15 秒; 对外 `GET /healthz` 检查数据库连通

## 4. 工程基线

### 4.1 配置项 (环境变量)

| 变量 | 默认 | 说明 |
|---|---|---|
| `MCLUB_ENV` | `prod` | `dev` 才允许 `seed-demo` 与调试输出 |
| `MCLUB_ADDR` | `127.0.0.1:8080` | 监听地址, 只允许回环 |
| `MCLUB_DB` | `host=/var/run/postgresql dbname=mclub user=mclub` | 连接串 |
| `MCLUB_DATA` | `/srv/maintclub/uploads` | 上传根目录 |
| `MCLUB_UPLOAD_QUOTA_MB` | `6144` | 上传总容量上限 |
| `MCLUB_COOKIE_SECURE` | `false` | 启用 HTTPS 后改为 `true` |
| `MCLUB_DEV_ASSETS` | 空 | 开发时指向 `Src/internal/web` 实现热读取 |
| `MCLUB_TRUSTED_PROXY` | `127.0.0.1` | 只信任此来源的 `X-Real-IP` |

### 4.2 CI 增强清单

在现有流水线 (格式, tidy, 测试, 编译, 压缩, 缓存清理) 的基础上增加. 全部在 GitHub Actions 里完成, 都不需要本机环境.

- [x] E1 `go vet ./...` 加入测试之前
- [x] E2 `services: postgres: image: postgres:18` (带 `pg_isready` 健康检查), 通过 `MCLUB_TEST_DSN` 传给测试; 每个测试包创建随机名称的临时数据库并跑完整迁移, 测试结束删除
- [x] E3 `go test -race ./...` 单独一步, 仅这一步设置 `CGO_ENABLED=1` (其余编译保持 `0`); 流水线其余部分不受影响
- [x] E4 `staticcheck` (固定版本, 通过 `go install` 装到工作区 `.bin`, 缓存在 `.gomodcache`)
- [x] E5 每周一次的 `govulncheck` 独立工作流 (定时, 不受 "只改 Docs 则跳过" 影响), 有漏洞则失败并发出通知
- [x] E6 冒烟: 启动压缩后的 `Publish/linux-x64/mclub` 连接服务容器, 请求 `/healthz` 和首页, 执行 `mclub migrate` 两次 (检查幂等), 然后正常退出
- [x] E7 标签 `v*` 触发 Release (D11). 实现时验证标签推送是否被 `paths-ignore` 影响, 必要时用 `workflow_dispatch` 作为补充入口
- [x] E8 `.github/dependabot.yml`: `gomod` 与 `github-actions` 每周检查
- [x] E9 把压缩前后体积, `go version -m` 摘要, 测试耗时写入 `$GITHUB_STEP_SUMMARY`, 便于观察体积趋势
- [x] E10 核对 `Src/internal/web/static/` 中第三方文件 (htmx) 的 sha256 与仓库清单一致, 防止被误改

不做 (已核实): 用 `qemu-user` 在 CI 里模拟无 AVX 的 CPU 运行 Go 程序. Go 官方不支持 qemu 用户态, 容易因无关原因失败. 无 AVX 的验证靠两点: 编译固定 `GOAMD64=v1` (CI 已检查构建信息), 以及每个里程碑在真机 J1800 上运行.

### 4.3 依赖清单 (全部为纯 Go)

| 依赖 | 引入阶段 | 用途 |
|---|---|---|
| `github.com/jackc/pgx/v5` | M0 | 数据库 |
| `golang.org/x/crypto` (bcrypt) | M1 | 密码 |
| `github.com/yuin/goldmark` | M2 | Markdown |
| `github.com/microcosm-cc/bluemonday` | M2 | HTML 清洗 (会带入 `golang.org/x/net`) |
| `golang.org/x/image` | M2 | 图片缩放与 WebP 解码 |

其他一律用标准库. 每增加一个依赖, 要在提交说明里写清理由. 目标是 `go.sum` 保持在几十行.

### 4.4 测试策略

| 层 | 内容 | 例子 |
|---|---|---|
| 单元 | 纯函数与业务规则 | 余额计算, 数量流水加总, 二元分词, 权限矩阵, EXIF 方向, Markdown 清洗语料 |
| 集成 | 真实 PG 18 | 迁移, 乐观并发冲突, 流水触发器, 全文检索, 领用与库存同事务回滚 |
| 处理器 | `httptest` | 未登录跳转, 越权返回 403, CSRF 拒绝, 表单校验 |
| 冒烟 | 压缩后的二进制 | 启动, 健康检查, 迁移幂等 |
| 手动 | 在 J1800 上走验收步骤 | 手机浏览, 内存观察 |

权限矩阵测试是核心资产: 以 "角色 (组长, 副组长, 组员) × 财务开关 × 操作" 做表驱动测试, 与 Plan1 第 3 节逐格对应. 以后任何权限改动必须先改表.

## 5. 资源预算 (J1800 / 2 GB / 32 GB)

内存 (目标值, M0 起每个里程碑实测):

| 项 | 预算 |
|---|---|
| 系统与内核 (Ubuntu Server, 精简服务后) | 约 250 MB |
| PostgreSQL (共享缓冲 128 MB, 加每个后端约 10 MB, 加辅助进程) | 约 300 MB |
| mclub (含 UPX 解压后的代码) | 小于 150 MB |
| Nginx | 约 15 MB |
| 页面缓存 (系统自动利用剩余内存) | 约 1 GB |
| 交换分区 | 1 GB 到 2 GB, `vm.swappiness` 调低 |

建议的 `postgresql.conf` 覆盖项 (放到 `conf.d/90-mclub.conf`):

```text
shared_buffers = 128MB
work_mem = 4MB
maintenance_work_mem = 64MB
effective_cache_size = 768MB
max_connections = 20
jit = off
max_parallel_workers_per_gather = 0
io_method = worker
io_workers = 2
max_wal_size = 512MB
autovacuum_max_workers = 2
log_min_duration_statement = 500ms
```

说明: PG 18 默认用 `worker` 方式做异步 I/O, 默认 3 个 I/O 进程; 2 核机器设 2 个. 不建议改 `sync`, 它只是兼容选项. 数据校验和在 PG 18 的 `initdb` 默认开启, 保持开启 (有利于发现磁盘位翻转, CPU 代价很小).

磁盘 (32 GB):

| 项 | 预算 |
|---|---|
| 系统与软件包 | 约 8 GB |
| 交换文件 | 2 GB |
| 数据库 (含 WAL) | 约 2 GB |
| 上传文件 | 6 GB (硬上限) |
| 备份: 数据库转储 7 份 | 约 1 GB |
| 备份: 上传目录镜像 | 6 GB |
| 日志 | 约 1 GB |
| 余量 | 不少于 6 GB (空闲低于 15% 就在状态页告警) |

性能目标 (J1800 实测, 10 人并发): 普通页面 p95 小于 300 ms; 搜索小于 200 ms (2000 页); 导出全部文档 zip 时进程内存增量小于 30 MB (流式写出, 不落临时文件).

## 6. 里程碑总览

```mermaid
flowchart LR
  M0[M0 骨架与上机] --> M1[M1 人员, 登录, 基础备份]
  M1 --> M2[M2 文档与文件]
  M2 --> M3[M3 库存]
  M3 --> M4a[M4a 活动]
  M4a --> M4b[M4b 任务]
  M4b --> M4c[M4c 首页聚合]
  M3 --> M5[M5 日志与维修]
  M4c --> M5
  M5 --> M6[M6 财务]
  M6 --> M7[M7 备份加固与试运行]
  M7 --> M8[M8 打磨与 v1.0]
```

| 里程碑 | 体量 | 一句话目标 |
|---|---|---|
| M0 | M + 装机 | 老机器上 Hello 跑通, CI 能快速反馈, 内存有数据 |
| M1 | L | 能登录, 权限正确, 每天有备份 |
| M2 | L | 文档系统可用, 解决 "资料散落" 这个最痛的点 |
| M3 | L | 器材与耗材账实相符 |
| M4a | M | 活动计划到总结 |
| M4b | M | 任务列表与看板 |
| M4c | S | 首页汇总 |
| M5 | M | 维修记录与库存联动 |
| M6 | L | 财务流水与余额 |
| M7 | M | 备份可恢复, 管理层试用一周 |
| M8 | M | 补足体验, 决定 HTTPS, 发布 v1.0.0 |

## 7. 里程碑详情

### M0 骨架与上机

目标: 在 J1800 上用 Nginx 反代跑通一个带数据库连通检查的空站点, 同时把 "快速反馈" 的基础设施建好.

**M0-A 仓库与 CI**

- [x] M0-01 (S) 子命令框架: `mclub serve | migrate | version`; `version` 输出提交哈希与构建时间 (CI 用 `-ldflags -X` 注入, 与现有 `-s -w` 并存)
- [x] M0-02 (S) `shared/config`: 读取第 4.1 节环境变量, 启动时打印生效配置 (不含机密)
- [x] M0-03 (S) `shared/db`: `pgxpool` 初始化, 连接参数 (上限 6), `Ping`
- [x] M0-04 (M) `shared/migrate`: D3 的迁移器, 含集成测试 (幂等, 并发加锁, 失败整体回滚)
- [x] M0-05 (S) `web`: `embed.FS`, 布局模板, 静态文件服务 (带 `ETag` 与长缓存, 文件名带内容哈希), 404 与 500 页面, 开发模式热读取
- [x] M0-06 (S) 中间件栈: 恢复 panic, 请求 ID, 访问日志, 安全头 (CSP, `nosniff`, `Referrer-Policy`, `X-Frame-Options`), 请求超时, 请求体上限
- [x] M0-07 (S) `/healthz` (数据库连通, 版本号), 优雅停机
- [x] M0-08 (M) CI 增强 E1 到 E10
- [x] M0-09 (S) CI 增加 `UPX_ENABLE` 开关 (D13)
- [x] M0-10 (S) 在一次提交里引入 `pgx` 并完成 `go-mod` 产物的下载提交闭环

**M0-B 服务器**

- [ ] M0-11 安装 Ubuntu Server 26.04 LTS (最小安装, 只装 SSH). 全盘 ext4, 不用 LVM 加密 (断电重启要能自动恢复). 创建 2 GB 交换文件
- [x] M0-12 **不要** 执行启用 `amd64v3` 架构变体的配置 (`APT::Architecture-Variants`), J1800 没有 AVX2, 开了会让软件包无法运行. 2026-11-16 已核对, 机器上没有这个配置
- [ ] M0-13 固定 IP 或内网域名; SSH 只允许密钥登录; `ufw` 只开 22 与 80
- [ ] M0-14 时间同步 (`systemd-timesyncd` 或 `chrony`): 审计日志依赖准确时间, 老主板电池可能没电
- [ ] M0-15 开启 `fstrim.timer`, journald 限制 200 MB, 自动安全更新 (`unattended-upgrades`, 只装安全更新, 不自动重启)
- [x] M0-16 `apt install postgresql-18 nginx` (Ubuntu 26.04 官方源已有 PostgreSQL 18, 无需添加 PGDG 源); 应用第 5 节配置; 创建角色 `mclub` 与库 `mclub`, `mclub_dev`. 沿用已安装的 PostgreSQL 18.6 与 Nginx 1.28.3, 没有另装版本
- [ ] M0-17 用 `systemctl list-units` 与 `smem` (或 `ps_mem`) 检查常驻服务, 关闭用不到的 (图形与硬件管理类, 云初始化等), 记录精简前后的内存
- [x] M0-18 编写 `Res/deploy/`: `mclub.service`, `nginx-mclub.conf`, `postgresql-mclub.conf`, `install.sh` (幂等), `deploy.sh`, `rollback.sh`
- [x] M0-19 Nginx: `server_tokens off`, `client_max_body_size 20m`, `Host $http_host`, `X-Real-IP`, 登录路径 `limit_req`, 文本压缩, `/healthz` 不记录访问日志. 站点本身作为默认站点, 以便用局域网 IP 访问
- [ ] M0-20 演练: Release 下载部署, `scp` 部署, 故意部署一个启动失败的版本观察自动回滚

**M0-C 度量**

- [ ] M0-21 记录: 空闲时 `free -m`, 应用 RSS, PostgreSQL 总 RSS, 冷启动时间 (UPX 开 / 关), 页面响应时间 (手机 Wi-Fi). 填入第 11 节
- [x] M0-22 根据 D13 判定线决定 Linux 版是否继续 UPX. UPX 压缩后的二进制在这台 J1800 上段错误, Linux 与 Windows 都不再压缩

验收:

- 手机连同一局域网, 打开站点能看到页面; `curl http://服务器/healthz` 返回数据库正常
- `systemctl restart mclub` 后 2 秒内恢复; 拔电源重启后服务自动起来
- 部署失败会自动回到上一版本
- 空闲时系统 `available` 内存不少于 1 GB

风险: UPX 实测不达标 (已设退路); 学校网络访问 GitHub 不稳定 (已设 `scp` 路径).

### M1 人员, 登录与基础备份

目标: 组长能建账号, 各角色看到该看的, 每天有数据库备份. 这是后续所有模块共用的底座, 要做扎实.

**共用底座 (shared)**

- [x] M1-01 (M) 会话: 表 `people.sessions`, 登录, 退出, 滑动与绝对过期, 改密码撤销其他会话, 每次请求校验人员状态
- [x] M1-02 (S) CSRF: `CrossOriginProtection` 加会话令牌 (D5), 带测试
- [x] M1-03 (M) 授权: `authz.Can(user, 权限, 资源)`, 权限常量, 权限矩阵表驱动测试 (见 4.4)
- [x] M1-04 (M) 审计: `audit.events(id, at, actor_id, actor_name, ip, action, entity, entity_id, summary, detail jsonb)`, 在业务事务内写入; 触发器禁止改删; 敏感字段 (密码哈希, 联系方式明文) 不写入 `detail`
- [x] M1-05 (S) 登录限速 (应用内 `(登录名, IP)` 指数退避) 与 `X-Real-IP` 仅在来自可信代理时采纳
- [x] M1-06 (M) 模板函数与组件: 时间 (UTC+8), 金额 (分转元), 分页, 提示消息, 表单错误, 空状态, 手机端导航
- [x] M1-07 (S) 首页组件注册机制: 各模块在 `routes.go` 注册 "我的待办 / 近期活动 / 低库存 ..." 组件, 首页按权限渲染 (M4c 才逐个接入)

**人员模块**

- [x] M1-08 (M) 表 `people.people`: 姓名, 登录名 (唯一, 小写), 密码哈希, 角色, 财务开关, 学号, 联系方式, 加入时间, 状态, 备注, 必须改密标志
- [x] M1-09 (M) 人员列表, 详情, 新建, 编辑; 退出与恢复; 重置密码 (生成一次性初始密码显示一次)
- [x] M1-10 (S) 字段可见性: 组员只能看到全员姓名与职务, 联系方式与学号仅管理层和本人可见 (在 service 层过滤, 不靠模板隐藏)
- [x] M1-11 (S) 不变量与换届: 在组的组长至少 1 人; 退出人员不能登录且立即失效; 单元测试覆盖 "先提拔后降级" 流程
- [x] M1-12 (S) 命令行: `mclub admin create <登录名>` (创建首个组长, 交互输入密码), `mclub admin reset-password <登录名>`
- [x] M1-13 (S) 个人资料页: 自己改密码, 改联系方式
- [x] M1-14 (S) 审计查看页: 组长全部, 副组长只读, 带按人员 / 动作 / 时间筛选
- [x] M1-15 (S) 人员 CSV 导入 (姓名, 登录名, 角色; 初始密码随机生成并一次性导出)

**基础备份**

- [x] M1-16 (S) `Res/deploy/backup.sh` 加 `mclub-backup.service` 与 `.timer`: 每天凌晨 `pg_dump -Fc` 到 `/srv/maintclub/backups/db/`, 保留 7 份, 失败写入 journald 并留下标记文件
- [x] M1-17 (S) 首次手动恢复到临时库 (`mclub_restore_test`) 并核对人员数量. 2026-11-16 从 `pre-local-20261116115852.dump` 恢复, `people.people` 为 1 行, 登录名 `07240506`, 角色组长

验收:

- 权限矩阵测试全部通过; 用 3 个账号 (组长, 副组长, 组员) 在手机上走一遍
- 被设为 "已退出" 的账号, 已登录的浏览器下一次请求即被踢出
- 组长无法把自己降级而留下 0 个组长
- 审计页能看到上面所有变更, 且数据库里手动 `UPDATE audit.events` 会被拒绝
- 备份文件生成, 恢复演练成功

风险: 权限设计偏差会污染后续所有模块, 所以矩阵测试必须先于页面实现.

### M2 文档与文件存储

目标: 把散落的资料收进系统, 多人维护不丢内容. 同时建立之后所有模块复用的文件存储.

**文件存储 (shared/files)**

- [x] M2-01 (M) D9 的完整实现: 上传, 类型与大小校验, 随机命名, 临时文件加改名, 元数据表 `files.files`, 授权下载, 孤儿清理, 容量统计与上限
- [x] M2-02 (M) 图片处理: `DecodeConfig` 限制像素, EXIF 方向解析 (自写约 60 行, 附样例文件测试), 缩放与重新编码, 单并发信号量
- [x] M2-03 (S) 路径穿越, 重名, 超大, 伪造扩展名的测试

**Markdown 与搜索**

- [x] M2-04 (M) `shared/mdown`: goldmark 加 bluemonday, XSS 载荷语料测试 (D7); 标题锚点, 表格, 任务列表; 站内图片引用语法 `![说明](/files/<id>)`
- [x] M2-05 (S) **搜索对比小实验**: 准备 20 页中文样例, 对 D8 的方案与 `ILIKE` 做召回与耗时对比, 确认或调整方案; 结论写进本节
- [x] M2-06 (M) `shared/search`: 二元分词函数 (含中英混排, 全角半角, 标点, 大小写), 查询构造, 单元测试覆盖边界
- [x] M2-07 (S) 预览接口 `POST /md/preview`

20 页样例里, "示波器" 和 "电源故障" 能被二元组命中, 单字 "焊" 走 `ILIKE`. 两边在这点数据上都是亚毫秒. 维持 D8: 多字用 `simple` 全文索引, 单字退回 `ILIKE`, 不引入分词扩展.

**文档模块**

- [x] M2-08 (M) 表: `docs.folders` (邻接表, 排序号), `docs.pages` (标题, 目录, 当前内容, `version`, 可见范围, 作废标志, 创建与最后修改人), `docs.page_versions` (每次保存的完整快照), `docs.tags`, `docs.page_tags`, `docs.page_files`; 生成列与 GIN 索引
- [x] M2-09 (M) 目录树 (递归查询), 页面查看, 新建, 移动, 重命名; 页面地址 `/docs/p/<id>` 加标题别名
- [x] M2-10 (M) 编辑: 文本框加实时预览; 拖入图片上传后自动插入引用; 保存时 `UPDATE ... WHERE id=$1 AND version=$2`
- [x] M2-11 (M) 冲突处理页: 显示 "我的修改 / 对方的修改 / 基础版本" 的行级差异, 选择 "覆盖 / 放弃 / 合并后重提交" (差异算法自写, 页面很小, 带测试)
- [x] M2-12 (S) 历史版本: 列表, 查看, 与当前对比, 还原 (还原也是一次新版本)
- [x] M2-13 (S) 可见范围: 全员 / 仅管理层; 在查询里过滤 (搜索, 目录, 导出, 原文地址都要过滤), 附带越权测试
- [x] M2-14 (S) 作废 (软删除): 填原因, 原记录保留, 页面树隐藏, 管理层可查看与恢复
- [x] M2-15 (S) 搜索页: 标题加正文, 分页, 命中片段高亮
- [x] M2-16 (S) 原文地址: `GET /docs/p/<id>.md` (`Content-Type: text/markdown; charset=utf-8`), 只对有权限者
- [x] M2-17 (M) 导出全部文档为 zip: 目录结构, `.md`, 图片相对路径改写, 流式写出, 受可见范围限制 (管理员导出全部, 其他人只导出可见部分)
- [x] M2-18 (S) 引入示例内容: 把 Docs 里的运维说明页放进系统, 作为 "第一批真实内容"

验收:

- 两人同时编辑同一页, 后保存者看到冲突页, 三种选择都可用, 且不会丢字
- 搜索 "示波器", "电源 故障", 单字 "焊" 都能得到合理结果
- 手机拍的竖直照片插入后方向正确, 文件小于 600 KB (常规 12 MP 照片)
- 导出 zip 在 J1800 上完成且进程内存增量小于 30 MB; 解压后用任意 Markdown 阅读器打开图片正常
- 仅管理层页面对组员: 目录不可见, 搜索不出, 直接访问地址返回 404 (不是 403, 避免泄露存在性)

风险: 二元分词召回不足 (M2-05 先验证); 图片处理占内存 (信号量与像素上限).

### M3 库存

目标: 器材, 工具, 消耗品, 元件的数量与状态账实相符.

- [x] M3-01 (M) 表 (本阶段不引入新依赖):
  - `inventory.locations` (邻接树)
  - `inventory.items` (编号唯一, 名称, 类别, 规格, 单位, 位置, 最低库存, 是否逐件管理, 照片, 备注, 作废标志)
  - `inventory.movements` (物品, 类型: 入库 / 领用 / 盘点调整 / 报废, 数量为带符号整数, 人员 ID 与姓名快照, 备注, 关联对象类型与 ID, 时间), 触发器禁止改删, 作废用冲销记录
  - `inventory.equipment_events` (逐件管理的器材: 借出, 归还, 送修, 修好, 报废; 状态由最新事件推出, 不单独存可被改乱的状态)
  - `inventory.stocktakes`, `inventory.stocktake_lines`
- [x] M3-02 (M) 数量规则 (单元与集成测试): 当前数量 = 流水加总; 出库前在同一事务内对物品行加锁再检查, 不允许出现负数; 并发领用测试
- [x] M3-03 (M) 物品增删改 (管理层), 位置树维护, 类别与筛选, 照片 (复用 M2 的图片处理)
- [x] M3-04 (M) 入库, 领用, 借出, 归还 (组员可登记); 人员与时间自动记录; 物品详情页展示流水与状态历史
- [x] M3-05 (M) 盘点: 创建盘点单, 逐项录入实际数量, 差异汇总, 确认后生成 "盘点调整" 流水
- [x] M3-06 (S) 低库存列表 (汇总数量小于最低库存), 注册为首页组件的数据源
- [x] M3-07 (S) 物品 CSV 导入 (含位置路径, 模板文件可下载) 与导出
- [x] M3-08 (S) 打印友好的物品标签页 (名称, 编号, 位置), 为以后二维码留接口: 物品页地址稳定为 `/inventory/items/<id>`
- [x] M3-09 (S) 搜索: 名称, 编号, 规格, 位置 (`ILIKE`, 需转义 `%` 与 `_`)

验收: 借还与领用后数量正确, 能完成一次完整盘点, 并发领用不会出现负库存, 报废器材不可再借出.

### M4a 活动

- [x] M4a-01 (M) 表: `events.events` (标题, 类型, 起止, 地点, 说明, 负责人, 总结, 状态), `events.attendees` (报名时间, 签到状态, 签到人), `events.event_files`
- [x] M4a-02 (M) 列表与详情; 月历视图 (服务端生成, 周一或周日起可配置); 手机上月历退化为按日列表
- [x] M4a-03 (S) 报名与取消 (截止前), 签到 (负责人或管理层代勾), 参加记录
- [x] M4a-04 (S) 活动总结 (Markdown) 与附件 (图片, PDF), 复用 M2 能力
- [x] M4a-05 (S) 时间处理规范: 表单按 UTC+8 解释, 存 UTC, 全天活动单独处理; 单元测试覆盖跨日与月末

### M4b 任务

- [x] M4b-01 (M) 表: `tasks.tasks` (标题, 说明, 类型, 状态, 优先级, 负责人, 截止日期, 关联活动, 创建人), `tasks.participants`, `tasks.comments`, `tasks.task_items` (关联器材)
- [x] M4b-02 (M) 列表: 筛选 "我的 / 类型 / 状态", 排序, 分页; 创建与编辑; 权限: 组员只能改自己负责或自己创建的 (复用 M1-03 矩阵)
- [x] M4b-03 (M) 看板: 按状态分列, htmx 拖动或按钮换列, 服务端保存; 没有 JS 时使用按钮
- [x] M4b-04 (S) 评论 (Markdown)
- [x] M4b-05 (S) 活动详情页 "一键创建关联任务"
- [x] M4b-06 (S) 状态变更审计 (谁在何时把状态改成什么)

### M4c 首页聚合

- [x] M4c-01 (S) 首页组件: 我的未完成任务, 即将到期, 近期活动, 低库存, 最近文档修改; 财务摘要等 M6 后接入
- [x] M4c-02 (S) 每个组件有数量上限和空状态; 查询均走索引, 首页总查询数不超过 8 条

验收 (M4 整体): 完整走一遍 "创建活动 → 关联任务 → 报名 → 签到 → 写总结 → 任务完成" 的流程, 首页每个人看到各自的内容.

### M5 日志与维修记录

- [x] M5-01 (M) 表: `logs.entries` (日期, 作者, 类型, 内容, 用时), `logs.repairs` (对象类型, 关联器材 ID 可空, 现象, 处理, 结果, 状态), `logs.repair_parts` (关联库存物品与数量), `logs.entry_files`
- [x] M5-02 (M) 三类日志的新建与编辑 (组员只能改自己的; 管理层可改全部); 日志列表与筛选
- [x] M5-03 (M) 更换元件: 在同一事务里调用库存的 "领用" 服务 (`关联对象 = 维修记录`); 编辑或作废维修记录时生成冲销流水; 集成测试覆盖回滚
- [x] M5-04 (S) 器材详情页嵌入该器材的全部维修保养历史 (通过 `inventory` 暴露的接口注册, 不让库存模块反向依赖日志模块)
- [x] M5-05 (M) 报修单: 状态受理 → 处理中 → 待取 → 完成; 外部来源与联系方式字段; 联系方式仅记录人与管理层可见 (待确认, 见第 10 节); 状态变更审计
- [x] M5-06 (S) 附图 (复用文件存储), 搜索 (`ILIKE`)

验收: 器材页能看到维修历史, 更换元件会扣库存, 作废维修记录会把库存加回来; 报修单可完整走完状态.

### M6 财务

规则明确, 以测试为先.

- [x] M6-01 (M) 先写 `finance` 的纯函数与测试: 手头 = 收入 - 支出 - 存入储蓄 + 取出储蓄, 储蓄 = 存入 - 取出, 按月按分类汇总, 作废记录不计入. 测试数据用手工账核对
- [x] M6-02 (M) 表: `finance.categories` (可配置, 带 "期初" 与 "报销" 预置项), `finance.entries` (类型, 金额分 `CHECK > 0`, 日期, 分类, 备注, 经手人, 作废时间, 作废人, 作废原因), `finance.entry_files`; 触发器 (D4)
- [x] M6-03 (M) 录入, 列表 (筛选: 月份, 类型, 分类, 经手人), 详情; 金额输入以 "元" 解析为分, 拒绝多于 2 位小数与负数
- [x] M6-04 (S) 作废 (必须填原因, 原记录保留, 列表灰显); 更正 = 新增一条
- [x] M6-05 (S) 附件: 发票 PDF 或照片, 可多个
- [x] M6-06 (M) 月度与分类统计页 (CSS 条形图), 余额卡片 (手头, 储蓄)
- [x] M6-07 (S) CSV 导出 (D10: BOM 加公式防注入)
- [x] M6-08 (S) 权限: 财务开关读写, 组长与副组长只读, 组员完全不可见 (菜单, 搜索, 首页, 直接地址均不可见); 矩阵测试补全
- [x] M6-09 (S) 首页财务摘要 (仅有权限者)
- [x] M6-10 (S) 审计: 每次新增, 作废, 导出都记录

验收: 导入一份真实或模拟的手工账 (至少 30 条), 余额与手工账逐月一致; 权限正确; 尝试用 SQL 修改或删除流水被触发器拒绝.

### M7 备份加固与试运行

- [ ] M7-01 (S) 上传目录增量备份: `rsync --link-dest` 每日快照或单份镜像 (按磁盘预算选择), 容量不超过预算
- [ ] M7-02 (M) **异机副本**: 每周从另一台设备拉取 (`scp` 或 `rsync` 到组长电脑 / 外接硬盘), 脚本放进 `Res/deploy`; 敏感库转储可用 `age` 或 `gpg` 加密 (可选)
- [ ] M7-03 (M) 系统状态页 (仅组长): 磁盘剩余, 上传目录大小, 数据库大小, 最近一次备份时间与结果, 进程内存, 版本号; 低于阈值有醒目提示
- [ ] M7-04 (M) 恢复演练: 在另一台机器或临时库按 README 步骤还原 "数据库加上传文件", 对比关键表行数与几份文档; 把步骤写进 `README.md`; 全程记录耗时
- [ ] M7-05 (S) 并发压力测试: 在你的电脑上用 `hey` 或 `bombardier` (单文件程序) 对首页, 搜索, 文档页施压 10 并发 5 分钟, 记录 RSS 与 p95
- [ ] M7-06 (S) 安全自查清单: 越权遍历 (用组员账号请求每个管理地址), CSRF, 上传伪装, XSS 语料, 路径穿越, Cookie 属性, 安全头
- [ ] M7-07 (S) 断电演练: 写入过程中拔电, 重启后数据库与服务自动恢复
- [ ] M7-08 (M) 小范围试用一周 (管理层 5 到 6 人), 收集问题 (记录在 GitHub Issues), 只修阻塞问题
- [ ] M7-09 (S) 用户手册雏形: 每个角色最常用的 5 件事 (写进系统的文档模块, 而不是仓库)

验收 (Plan1): 备份可恢复; 管理层试用一周无阻塞问题.

### M8 打磨与 v1.0

- [ ] M8-01 全站搜索入口 (文档全文 + 其他模块 `ILIKE`, 按权限过滤)
- [ ] M8-02 HTTPS 决策: 评估手机信任内部 CA 的成本, 以及是否需要页内扫码; 不需要则继续 HTTP 并在 README 说明风险边界
- [ ] M8-03 二维码标签 (纯 Go 生成 PNG 或 SVG, 打印版面)
- [ ] M8-04 手机体验回归 (360 到 414 px), 触控目标大小, 暗色模式 (可选)
- [ ] M8-05 README 补全: 安装, 升级, 回滚, 备份与恢复, 常见故障
- [ ] M8-06 整理 CHANGELOG, 打标签 `v1.0.0`

## 8. 横切要求

安全清单 (每个模块上线前逐项核对):

- 每个处理器先过认证与授权, 授权在 service 层, 不依赖模板隐藏按钮
- 无权限的对象返回 404 而不是 403 (文档, 财务)
- 所有 SQL 参数化; 动态排序列用白名单
- 所有输出走 `html/template` 自动转义; 绝不对用户输入使用 `template.HTML`
- 文件下载强制 `attachment` 与 `nosniff`, 图片内联也经过重新编码
- 所有重要写入有审计记录; 作废不删除
- 日志不记录密码, 令牌, 联系方式

数据保留与规模估计:

- 审计表按行数估计很小, 每年约几万行; 暂不做归档, M7 之后根据实际增长再评估
- 文档历史版本存完整快照, 以 5000 字 (约 15 KB) 的页面 × 每页 50 个版本 × 500 页计约 375 MB, 若实际逼近预算再改为 "近 N 个版本完整, 更早的压缩"

学期交接 (换届) 场景: 新组长创建后, 旧组长先改角色再退出; 数据全部保留, 报表按日期范围筛选; 这一点在 M1 与 M7 的手册里都要写清.

## 9. 风险登记

| # | 风险 | 影响 | 应对 |
|---|---|---|---|
| R1 | UPX 带来更高内存与更慢启动 | 老机器启动慢, 内存紧 | M0 实测, 有开关 (D13) |
| R2 | 无本地编译, 反馈慢 | 开发效率 | D12 开发循环, E1 到 E6, 开发模式热读取 |
| R3 | 同盘备份 | 硬盘坏了全没了 | M7-02 异机副本, M1 就有每日备份 |
| R4 | GitHub 访问受限 | 无法部署, 无法下载产物 | `scp` 路径与 M0-20 演练; 依赖 `go.sum` 在云端生成不受影响 |
| R5 | 中文搜索效果不够 | 文档模块价值下降 | M2-05 先验证; 方案可替换, 接口留在 `shared/search` |
| R6 | 图片处理耗内存 | 进程被杀 | 像素上限, 单并发, `GOMEMLIMIT`, `MemoryMax` |
| R7 | HTTP 局域网下 CSRF 保护较弱 | 内网被诱导提交 | D5 的令牌与 `Host` 保留; 敏感操作要求重新输入密码 (可选, 财务作废, 角色变更) |
| R8 | 权限遗漏 | 数据泄露 | 权限矩阵测试, 越权遍历测试, 返回 404 |
| R9 | 公开仓库误提交真实数据 | 隐私泄露 | 测试与示例全用假数据, `.gitignore` 排除 `*.env`, `*.dump`, `uploads/` |
| R10 | 一个人维护, 交接困难 | 毕业后无人能接 | 文档放系统里, 部署脚本幂等, 代码结构一致, README 写清 |
| R11 | 断电损坏数据 | 数据不一致 | 保持 `fsync` 开启, ext4 日志, 校验和, M7-07 演练; 建议接一个小 UPS |
| R12 | 模块互相耦合 | 修改一处牵连多处 | D1 的跨模块规则, 通过接口与事务参数协作 |
| R13 | 范围蔓延 | 做不完 | "明确不做" 清单不变; 新需求先进入第 10 节后备清单, 不插队 |
| R14 | `go.sum` 回写流程打断节奏 | 多一次提交 | 依赖集中引入, 每个里程碑一次 |
| R15 | 学校对个人信息管理有要求 | 合规 | 学号与联系方式可选, 仅管理层与本人可见, 退出后可清空 |

## 10. 待确认与后备清单

新增的待确认项 (已写默认值, 不改则按默认):

| 问题 | 默认 |
|---|---|
| 报修单里外部人员的联系方式谁能看 | 记录人与管理层 |
| 是否需要重复活动 (每周例会) | 先不做, 用 "复制到下一周" 按钮; 列入后备 |
| 开发库放哪里 | 服务器上的 `mclub_dev`, 通过 SSH 隧道访问 |
| 上传总容量上限 | 6 GB |
| 图片长边上限 | 2000 px |
| 文档历史版本保留数 | 全部保留, 用到预算 30% 再处理 |
| 是否要求敏感操作二次输入密码 | 否 (R7 中列为可选) |

后备清单 (v1.0 之后再排):

- PDF 导出 (纯 Go 库, 按需选型)
- 站内通知 (任务分配, 活动提醒), 不做邮件
- 页面双向链接 `[[页面名]]`
- 活动重复规则
- 学期统计报表 (参加次数, 工时)
- 器材二维码扫码借还流程
- 暗色模式
- 备份加密与自动校验恢复
- htmx 4.x 升级 (2027 年再评估)

## 11. 实测记录 (随开发填写)

| 时间 | 版本 | 空闲 `available` 内存 | mclub RSS | PostgreSQL 总 RSS | 冷启动 | 页面 p95 | 备注 |
|---|---|---|---|---|---|---|---|
| 2026-11-16 | dev 897406b, 无 UPX | 1384 MB | 15 MB | 未单列 | 1 秒内 | 未测手机 | UPX 压缩件段错误, 已取消. 根卷 27 GB, 可用 23 GB |
| | | | | | | | |

## 12. 调研来源

- Ubuntu 26.04 LTS: 提供 PostgreSQL 18, amd64 基线无扩展要求, `amd64v3` 为可选变体: [Ubuntu Server 下载页](https://ubuntu.com/download/server), [支持的架构](https://ubuntu.com/project/docs/how-ubuntu-is-made/concepts/supported-architectures/), [26.04 发布说明](https://documentation.ubuntu.com/release-notes/26.04/), [`postgresql-18` 在 26.04 的版本](https://code.launchpad.net/ubuntu/resolute/amd64/postgresql-18)
- PostgreSQL 18: [发布说明](https://www.postgresql.org/docs/18/release-18.html) (`uuidv7()`, 虚拟生成列, 异步 I/O), [资源配置](https://www.postgresql.org/docs/current/runtime-config-resource.html), [AIO 调优](https://vondra.me/posts/tuning-aio-in-postgresql-18/), [PG 18 扩展问题清单](https://wiki.postgresql.org/wiki/PostgreSQL_18_Extension_Bugs)
- 中文检索: [zhparser, pg_jieba, pg_trgm 对比](https://www.phpxmn.com/database/postgresql/data-types/full-text/chinese.html), [pg_trgm 与 pg_bigm 实测](https://ycchuang.ghost.io/boosting-chinese-search-efficiency-postgresql-testing-pg_trgm-pg_bigm/)
- Go 与 pgx: [pgx](https://github.com/jackc/pgx/), [pgx 连接数与内存](https://github.com/jackc/pgx/issues/1127), [goose](https://github.com/pressly/goose)
- `go:embed` 不能引用父目录: [embed 文档](https://pkg.go.dev/embed), [golang/go#46056](https://github.com/golang/go/issues/46056)
- CSRF: [`net/http` CrossOriginProtection 源码](https://github.com/golang/go/blob/go1.26.4/src/net/http/csrf.go), [gorilla/csrf CVE-2025-47909](https://github.com/advisories/GHSA-82ff-hg59-8x73), [Fetch metadata 仅发往可信来源](https://developer.mozilla.org/en-US/docs/Web/HTTP/Guides/Fetch_metadata), [HTTP 局域网地址下缺失 `Sec-Fetch-Site` 的复现](https://repro-log.blogspot.com/2026/10/sec-fetch-site-is-missing-on-same.html)
- Markdown: [goldmark 与 bluemonday 的安全用法](https://gohacks.dev/library/go-goldmark-markdown)
- 图片: [`image` 包安全说明](https://pkg.go.dev/image), [缩略图与 EXIF 方向](https://gohacks.dev/library/go-thumbnails), [`image/jpeg` 不处理 EXIF](https://fileformats.dev/jpeg/libraries/go/image-jpeg)
- UPX 代价: [内存与启动时间测量](https://usrme.xyz/posts/using-upx-for-compression-might-work-against-you/), [UPX 官方说明](https://github.com/upx/upx/issues/466), [k0s 评估](https://github.com/k0sproject/k0s/pull/5470)
- htmx: [v2.0.11](https://github.com/bigskysoftware/htmx/releases/tag/v2.0.11), [4.0 发布公告](https://four.htmx.org/announcements/2026-08-28-htmx-4.0.0-is-released)
- Go 版本变化: [Go 1.26](https://go.dev/doc/go1.26), [Go 1.27](https://go.dev/doc/go1.27)
- Go 在 qemu 用户态不受支持: [golang/go#36981](https://github.com/golang/go/issues/36981), [golang/go#20763](https://github.com/golang/go/issues/20763)
- Actions: [服务容器示例](https://github.com/emarsden/pg-el/blob/main/.github/workflows/test-pgv18.yml), [Release 工作流示例](https://github.com/containerd/shimtest/blob/main/.github/workflows/release.yml), [artifact 下载 API](https://docs.github.com/en/rest/actions/artifacts)

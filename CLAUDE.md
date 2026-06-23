# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概要

玩家网络拨测平台 — 游戏 DevOps 工具。客服发链接给反馈「进不去游戏」的玩家，玩家点开网页自动拨测各游戏模块 API 的可达性和延迟，DevOps 凭截图中的测试编号在后台查询/分析。

- 后端 Go，单 ClickHouse 数据库，可选 Redis 限流
- 玩家页配置驱动（后台 CRUD 目标），浏览器端 no-cors/cors 探测 + 流式上报
- 判定矩阵用 baseline 对照组防误判（国内/国际基线 vs 我方业务）
- GeoIP 离线富化：ip2region（国内省/市/ISP）+ MaxMind mmdb（国际，待集成）

## 常用命令

```bash
# 本地开发环境
docker-compose up -d                                   # 起 ClickHouse + Redis

# 构建与运行
go run ./cmd/server/ config.yaml                       # 启动服务
go build -o server ./cmd/server/ && ./server config.yaml

# 测试
go build ./...                                         # 检查编译
go vet ./...                                           # 静态分析

# 依赖
go mod tidy                                            # 依赖变更后重解析

# 访问
open http://localhost:8080/                            # 玩家测试页
open http://localhost:8080/admin-page                  # 管理后台
```

## 架构

```
浏览器(玩家) ──GET /api/session──▶ Go(gin) ──geoip──▶ ClickHouse(tl_session)
             ◀─{id,ip,geo,targets}──    │                       │
             ──POST /api/report────▶     │                       │
             ◀─{verdict}────────────     ▼                       ▼
                                      ClickHouse(tl_probe_result, tl_target)
管理后台 ──/admin/*────▶ Go(JWT/static token)
```

**库职责：**
- ClickHouse（唯一数据库）：`tl_target`（配置）、`tl_session`（session 元数据）、`tl_probe_result`（拨测事件）——全部 ReplacingMergeTree
- Redis：限流（不可用时自动放通，不阻塞业务）

**核心模式 — 可变字段不用 UPDATE：** `tl_session` 和 `tl_target` 的更新全用 **INSERT 新版本行 + version+1**，ReplacingMergeTree 取最新版本，完全不触发 ClickHouse mutation。查询最新用 `ORDER BY version DESC LIMIT 1` 或 `argMax`。

**玩家页流程：**
1. 页面加载 → `GET /api/session?g=&s=&t=` → 后端签发 `MMDD-XXXXXX` 编号、取 IP、ip2region 富化 geo、写 session、返回目标列表
2. 逐目标探测（按 mode cors/no-cors、repeat_count 次、cache_bust 可选、AbortController 控超时）
3. 每目标完成即 `POST /api/report` 流式上报 → 后端写 probe_result + geo 冗余
4. 最终 `POST /api/report` 触发服务端 verdict 计算 → 页面显示结论
5. sendBeacon 兜底

**判定矩阵（`internal/verdict/`）：**
- 国内基线（如 baidu）任一 reachable → 国内网络正常
- 国际基线（Google/CF/ipinfo/ip.sb）任一 reachable → 国际可达
- 两者 OK + 业务全挂 → `OUR_API_DOWN`
- 两者 OK + 业务部分挂 → `PARTIAL_FAIL`
- 国内 OK + 国际不行 → `PLAYER_CROSSBORDER_BLOCKED`
- 两者都挂 → `PLAYER_NET_DOWN`

**目标配置（`tl_target`）：** 每个目标设 role（baseline/business）、mode（cors/no-cors）、group_name（模块分组）、extract_rule（cors 目标提取 IP/geo 的 JSONPath）。后台 CRUD 用 INSERT 新版本行（version+1）。

## 关键文件

| 文件 | 职责 |
|---|---|
| `cmd/server/main.go` | 入口，gin 路由注册、优雅关停 |
| `cmd/server/config.go` | YAML 配置加载与默认值 |
| `internal/geoip/geoip.go` | ip2region xdb 内存加载，`Search(ip)` 返回国家\|省\|市\|ISP |
| `internal/store/clickhouse.go` | ClickHouse 连接、DDL、全部 CRUD、种子数据（7 条默认目标） |
| `internal/session/service.go` | Session 编号签发（base32）、UA 解析、verdict/note 追加更新 |
| `internal/target/service.go` | 目标配置 CRUD（追加版本、软删除 disabled=0） |
| `internal/probe/service.go` | 上报处理、snapshot 校验、触发 verdict |
| `internal/verdict/verdict.go` | 5 种 verdict code 判定逻辑 |
| `internal/auth/auth.go` | JWT + 静态 admin token 双模式 |
| `internal/ratelimit/ratelimit.go` | Redis INCR + EXPIRE 固定窗口，Redis 挂掉容错放通 |
| `internal/api/handler.go` | HTTP handlers（玩家侧 session/report、后台 CRUD/查询/统计） |
| `internal/api/middleware.go` | 限流中间件（RateLimitSession/Report）、admin JWT 鉴权中间件 |
| `web/player/index.html` | 玩家拨测页（配置驱动、session 编号大字、流式上报、verdict 条） |
| `web/admin/index.html` | 后台 SPA（目标 CRUD、按编号查询、多维搜索、统计看板） |
| `migrations/001_init.sql` | DDL 参考（应用启动时 `store.Init` 自动执行） |

## 配置要点

`config.yaml` 关键项：
- `clickhouse.password` — 需与 `docker-compose.yml` 中 `CLICKHOUSE_PASSWORD` 一致
- `geoip.ip2region_v4/v6` — xdb 文件路径（当前放项目根目录）
- `auth.admin_token` — 后台登录凭证（内部工具，简单 token）
- `auth.jwt_secret` — JWT 签名密钥
- `ratelimit.session_per_ip_per_min` — 默认 5

## GeoIP 集成

ip2region 已可用（国内省/市/ISP）。MaxMind mmdb（`GeoLite2-Country.mmdb` + `GeoLite2-ASN.mmdb`）已下载但 Go 端未集成——`internal/geoip/geoip.go` 仅查 ip2region。国际 IP 当前返回 country="未知"。后续集成需引入 mmdb 读取库（pkg.go.dev 搜 `maxminddb`），在 `Lookup()` 中 ip2region 无结果时回退查 mmdb。

## ClickHouse ReplacingMergeTree 注意事项

- 去重是异步的（后台 merge），查询需显式用 `ORDER BY version DESC LIMIT 1` 或 `FINAL` 或 `argMax`
- 无 UNIQUE 约束：session 编号冲突靠生成后点查存在性 + 空间足够大（10^9/天）避免
- ORDER BY 决定去重 key 和存储排序，当前 `tl_session ORDER BY session_id`，点查快；时间范围扫描全表但本量级足够

# TestLink — 玩家网络拨测平台

面向游戏 DevOps 的轻量级网络诊断工具。客服将测试链接发给反馈「进不去游戏」的玩家，玩家打开网页即自动拨测各游戏模块 API 的可达性和延迟，DevOps 凭截图中的测试编号在后台查询完整数据和分析。

## 核心功能

- **玩家侧**：一个链接打开，自动探测配置的所有目标，显示可达性、延迟、冷连接耗时，带唯一测试编号供截图回传。支持 4 语言（简中 / 繁中 / English / Tiếng Việt）自动跟随浏览器
- **后台侧**：目标配置完全自定义（分组名、角色均可自由输入）、按编号或条件搜索历史 session、展开每次拨测的完整详情（分段耗时表、STATUS/HEADERS/BODY）
- **自动判定**：基线站点对照 + 延迟阈值 → 玩家网络故障 / 我方 API 故障 / 部分异常 / 正常；未完成的目标不会被误判为全部故障
- **隐私安全**：所有探测由玩家浏览器直连目标发起，后端不代理、不中转请求

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go + Gin |
| 数据库 | ClickHouse（ReplacingMergeTree，单库零依赖） |
| 缓存/限流 | Redis（不可用时自动降级，不影响服务） |
| GeoIP | ip2region（中国大陆省/市/ISP）+ MaxMind mmdb（港澳台及国际，交叉校验） |
| 前端 | 单 HTML，零构建，无框架 |

## 快速开始

### Docker 部署（推荐）

```bash
# 1. 准备 GeoIP 库（放到项目根目录）
#    ip2region: https://github.com/lionsoul2014/ip2region → data/ip2region_v4.xdb
#    MaxMind GeoLite2（可选）: https://dev.maxmind.com/geoip/geolite2-free-geolocation-data

# 2. 编辑 config.yaml

# 3. 启动 ClickHouse + Redis + App
docker compose up -d

# 4. 访问
#    玩家页：  http://localhost:8080/
#    后台：    http://localhost:8080/admin-page
```

首次启动自动建表并写入默认探测目标。

### 手动部署三容器

```bash
# ClickHouse
docker run -d --name testlink-ck -p 9000:9000 \
  -e CLICKHOUSE_DB=testlink -e CLICKHOUSE_USER=testlink \
  -e CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1 -e CLICKHOUSE_PASSWORD="testlink123" \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data/clickhouse:/var/lib/clickhouse \
  clickhouse/clickhouse-server:latest

# Redis
docker run -d --name testlink-redis -p 6379:6379 \
  -e TZ=Asia/Shanghai redis:alpine

# App（推荐用环境变量覆盖连接地址；也可以直接改 config.yaml）
docker run -d --name testlink-app -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  -e TESTLINK_CLICKHOUSE_HOST="服务器内网IP" \
  -e TESTLINK_REDIS_HOST="服务器内网IP" \
  -e TESTLINK_ADMIN_TOKEN="your-token" \
  -e TESTLINK_JWT_SECRET="random-string" \
  -v $(pwd)/config.yaml:/app/config.yaml \
  whpan/testlink:latest
```

### 本地开发

```bash
docker compose up -d                     # 起 ClickHouse + Redis
go run ./cmd/server/ config.yaml         # Go 直接跑
```

## 构建镜像

```bash
docker build -t testlink:latest .
# 多架构推送
docker buildx build --platform linux/amd64,linux/arm64 -t whpan/testlink:latest --push .
```

## 配置要点

```yaml
server:
  trusted_proxy_cidrs: ["127.0.0.1", "10.0.0.0/8"]  # 生产反代/负载均衡 CIDR，避免信任任意 XFF

clickhouse:
  host: "10.x.x.x"        # 服务器内网 IP
  password: "testlink123"

redis:
  host: "10.x.x.x"        # 同上

auth:
  admin_token: "your-token"      # 后台登录密码
  jwt_secret: "random-string"   # JWT 签名密钥

geoip:
  ip2region_v4: "ip2region_v4.xdb"
  ip2region_v6: "ip2region_v6.xdb"
  maxmind_country: "GeoLite2-Country.mmdb"   # 可选
  maxmind_asn: "GeoLite2-ASN.mmdb"           # 可选

ratelimit:
  session_per_ip_per_min: 5     # 单 IP 每分钟最多创建 5 个 session
```

也支持用环境变量覆盖 `config.yaml` 中的连接和鉴权配置，便于 Docker 部署：

| 环境变量 | 对应配置 |
|---|---|
| `TESTLINK_PORT` | `server.port` |
| `TESTLINK_TRUSTED_PROXY_CIDRS` | `server.trusted_proxy_cidrs`，逗号分隔 |
| `TESTLINK_CLICKHOUSE_HOST` | `clickhouse.host` |
| `TESTLINK_CLICKHOUSE_PORT` | `clickhouse.port` |
| `TESTLINK_CLICKHOUSE_DATABASE` | `clickhouse.database` |
| `TESTLINK_CLICKHOUSE_USERNAME` | `clickhouse.username` |
| `TESTLINK_CLICKHOUSE_PASSWORD` | `clickhouse.password` |
| `TESTLINK_REDIS_HOST` | `redis.host` |
| `TESTLINK_REDIS_PORT` | `redis.port` |
| `TESTLINK_REDIS_PASSWORD` | `redis.password` |
| `TESTLINK_ADMIN_TOKEN` | `auth.admin_token` |
| `TESTLINK_JWT_SECRET` | `auth.jwt_secret` |


## 目标配置说明

后台可自由增删改目标。每个目标有两个关键属性：

- **角色**：`基线`（第三方参照站点，用于判断玩家自身网络）或 `业务`（我方 API，出问题就是故障）
- **分组**：自由命名，纯粹用于 UI 归类展示，不影响判定逻辑
- **模式**：`cors`（可读取响应状态码/头/体）或 `no-cors`（仅连通性检测）
- **延迟阈值**：`latency_warn_ms`，0 表示不检查；大于 0 时，用暖连接平均延迟（第 2 次及以后，若只有 1 次则用第 1 次）判断是否偏慢

## 判定逻辑

| 场景 | 判定 |
|---|---|
| 基线全挂 | 玩家自身网络异常 |
| 基线通 + 业务全挂 | 我方 API 全部不可达 |
| 基线通 + 业务部分挂 | 部分模块异常（列出具体名称） |
| 基线通 + 业务可达但超过延迟阈值 | 部分模块异常（列出延迟较高目标） |
| 上报不完整（例如玩家提前关闭页面） | 数据不完整，不做“全部不可达”误判 |
| 基线通 + 业务全通 | 当前网络连接测试正常 |

## 项目结构

```
├── cmd/server/          # 入口，Gin 路由注册
├── internal/
│   ├── model/           # 共享类型
│   ├── geoip/           # ip2region + MaxMind 离线 GeoIP
│   ├── store/           # ClickHouse DDL + CRUD
│   ├── session/         # Session 创建/查询/更新
│   ├── target/          # 目标配置 CRUD
│   ├── probe/           # 上报处理
│   ├── verdict/         # 自动判定
│   ├── ratelimit/       # Redis 限流
│   ├── auth/            # JWT 鉴权
│   └── api/             # HTTP handlers + middleware
├── web/
│   ├── player/          # 玩家测试页（i18n）
│   └── admin/           # 管理后台
├── migrations/          # ClickHouse DDL 参考（启动时自动执行）
├── Dockerfile
├── docker-compose.yml   # ClickHouse + Redis + App 全栈
├── config.yaml          # 应用配置
└── README.md
```

## 浏览器能力边界

所有探测由玩家浏览器 `fetch()` 直连目标，后端不代理请求。

| 能力 | cors 目标 | no-cors 目标 |
|---|---|---|
| 网络可达性 | ✅ | ✅ |
| 超时 vs 快速失败 | ✅ | ✅ |
| HTTP 状态码 | ✅ | ❌（opaque） |
| 响应头/响应体 | ✅ | ❌ |
| DNS/TCP/TLS 分段耗时 | ✅（需 TAO 头） | ❌ |

## i18n / 多语言

玩家页自动跟随 `navigator.language`，支持：

| 浏览器语言 | 显示语言 |
|---|---|
| `zh-CN` / `zh-SG` | 简体中文 |
| `zh-TW` / `zh-HK` | 繁體中文 |
| `en` / `en-US` | English |
| `vi` / `vi-VN` | Tiếng Việt |

未匹配的语言降级到英文。

## License

MIT

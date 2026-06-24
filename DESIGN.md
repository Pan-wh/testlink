# 玩家网络拨测平台 设计文档

> 状态：已实现，持续迭代
> 版本：v1.0 · 2026-06-23

## 目录

1. [背景与目标](#1-背景与目标)
2. [核心设计理念](#2-核心设计理念)
3. [浏览器能力边界](#3-浏览器能力边界)
4. [整体架构](#4-整体架构)
5. [玩家测试页设计](#5-玩家测试页设计)
6. [判定矩阵](#6-判定矩阵)
7. [数据模型](#7-数据模型)
8. [API 接口清单](#8-api-接口清单)
9. [限流与安全](#9-限流与安全)
10. [GeoIP 方案](#10-geoip-方案)
11. [技术栈](#11-技术栈)
12. [项目骨架与部署](#12-项目骨架与部署)
13. [分期计划](#13-分期计划)
14. [后续可选项](#14-后续可选项)

---

## 1. 背景与目标

游戏 DevOps 经常收到玩家反馈「进不去游戏」。需要一个工具：

- **玩家侧**：客服发一个链接，玩家点开自动测试各游戏模块 API 的网络可达性，截图回传。
- **DevOps 侧**：凭截图中的**测试编号**在后台查询这次测试的完整数据（玩家 IP/归属地/运营商 + 各目标结果 + 自动判定结论），并做跨玩家多维分析。

### 关键约束（已确认）

1. **仅做 HTTPS、仅浏览器探针**（游戏自有其他上报检测，不重复造）。
2. **目标 API 不可修改**：部分是研发 API，不能加 CORS / `Timing-Allow-Origin` 头。
3. 目标包含各阶段 HTTPS 链接（CDN 更新列表、区服列表、SDK 等），且**全部后台可自定义增删**。
4. 后端用 **Go**。
5. 公司内部使用，**长期留存**（IP 可明文存，仅做存储分层卫生）。
6. **需要限流**防滥用。

---

## 2. 核心设计理念

### 2.1 截图 → 编号 → 后台查询（主流程轴心）

整个玩家页围绕「截图能看清、编号能对上、数据已落库」设计：

```
客服发链接 → 玩家点开 → 后端签发测试编号并取IP/富化geo
            → 页面自动拨测各目标 → 每个完成即上报落库
            → 玩家截图（含编号+IP+归属地+结果表）回传
            → DevOps 凭编号后台查询 → 看完整数据+判定结论
```

- 测试编号在页面加载时由后端签发，**编号生成即落库**（哪怕探测还没完成，DevOps 也能查到 IP/归属地）。
- 结果**随完成流式上报**，`sendBeacon` 兜底，保证截图时数据已在后端。

### 2.2 对照组（baseline）防误判

只测我方接口会误判：玩家自身网络断了，我方接口全挂 ≠ 我方故障。加入高可用第三方作为**对照组**：

- **baseline 组**：Cloudflare / Google / ipinfo / ip.sb（及建议的国内站点如 baidu），判断玩家自身网络是否正常。
- **business 组**：我方 API。

对照组**只依赖 no-cors 可达性**，不依赖第三方是否开 CORS → 永远可靠，适合做标尺。详见 [§6 判定矩阵](#6-判定矩阵)。

---

## 3. 浏览器能力边界

研发 API 不可改 → 只能 `fetch(url, {mode:'no-cors'})`。能力差异：

| 能力 | 自有 CORS 目标（如 `/v1/ip`） | 研发 API（no-cors，不可改） |
|---|---|---|
| 网络可达性（能不能连上） | ✅ | ✅ |
| 总延迟 | ✅ | ✅ |
| 超时 vs 快速失败 区分 | ✅ | ✅（AbortController 超时 vs 快速 reject） |
| HTTP 状态码（200/500） | ✅ | ❌（opaque，看不到） |
| DNS/TCP/TLS 分段耗时 | ✅（需 TAO） | ❌（跨域无 TAO 被置 0） |
| 浏览器实际解析到的 IP | ❌（浏览器不暴露） | ❌ |
| DoH 查域名解析 IP（上下文） | ✅ 可选 | ✅ 可选（Phase 3） |

### 关键结论

1. **no-cors 下「成功」≠「HTTP 200」**：服务器返回 500/404 也只给 opaque 响应，`fetch` 照样 resolve。研发 API 只能判断**网络层通不通**，判断不了业务状态码。
2. **「超时」vs「快速失败」是关键诊断信号**：
   - 静默丢包/路由黑洞 → 挂到超时（如 5s）才 abort → `timeout`
   - DNS 失败/连接被拒/TLS 不匹配 → 几十毫秒内快速 reject → `fast_fail`
   - 两种故障方向完全不同，必须采。
3. **分段漏斗（DNS/TCP/TLS）仅对带 TAO 的自有目标可用**，研发 API 拿不到。分段字段设为**可空**——有就存。
4. **非标端口 HTTPS 可测**：`https://host:9001` 浏览器允许（9001 非 bad-port），能测源站直连端口可达性——游戏 DevOps 经典场景（企业网/移动网封非标端口）。

### 探测结果枚举 `outcome`

| 值 | 含义 |
|---|---|
| `reachable` | fetch resolve（网络层通，含 4xx/5xx） |
| `timeout` | AbortController 超时（静默丢包/黑洞） |
| `fast_fail` | 超时前快速 reject（DNS 拒绝/TCP refused/TLS 错误） |
| `http_error` | CORS 目标可见的 HTTP 错误状态（≥400） |

---

## 4. 整体架构

```
┌──────────────┐   ①GET /api/session(签发编号+取IP+富化geo+返回目标列表)
│ 玩家测试页    │ ◀──────────────────────────────────────────────────┐
│ (浏览器,演进  │                                                     │
│  自旧index)  │   ②按目标 mode(cors/no-cors) 探测 repeat 次         │
└──────┬───────┘   ③每目标完成即 POST /api/report (流式上报)         │
       │                  ④sendBeacon 兜底                           │
       ▼                                                     │
┌──────────────────┐   ⑤GeoIP离线富化(已随session完成)              │
│ 接入 API (Go)     │ ──session+判定────────▶ ┌──────────────────┐  │
│ - 高并发写入      │ ──results(geo冗余)──────▶│ ClickHouse        │  │
│ - 限流(Redis)     │ ──session元数据────────▶│ (config+session+  │  │
│ - 目标白名单校验  │ ──配置(目标)───────────▶│  probe_result)    │  │
│ - JWT(管理员)     │                         └──────┬───────────┘  │
└──────────────────┘                                │ ⑥查询/聚合    │
                                                    ▼              │
                                          ┌──────────────────┐     │
                                          │ 管理后台 (前端)   │◀────┘
                                          │ - 目标CRUD        │
                                          │ - 按编号查询       │
                                          │ - 多维搜索         │
                                          │ - 记录详情         │
                                          └──────────────────┘
```

### 库职责分工（仅 ClickHouse + Redis，不引入 MySQL）

| 库 | 职责 | 理由 |
|---|---|---|
| **ClickHouse** | 配置（目标）、session 元数据、probe_result 事件（geo 反范式冗余） | 单库统一维护；session/事件是时序+OLAP 主场；配置表极小、变更罕见 |
| **Redis**（可选推荐） | 限流、实时计数 | 轻量 |

> **为何能去掉 MySQL**：session/config 的可变字段（verdict、note、symptom）用 **append + ReplacingMergeTree** 模式——更新即插入新版本行（version+1），查询取最新版本，**完全不用 ALTER UPDATE mutation**。点查 session_id 用 `ORDER BY session_id` 主键，本量级足够快。管理员鉴权用 **JWT / 静态 token**（无状态，不落 DB），避开 ClickHouse 不擅长的 OLTP 场景。无事务需求，丢 MySQL 无损失。

---

## 5. 玩家测试页设计

### 5.1 页面布局（沿用旧 index 风格，移动端自适应）

```
┌─────────────────────────────────────────────┐
│  网络诊断                                    │
│  测试编号：0622-K7QX9P        ← 大字、截图必看清│
│  您的IP：120.x.x.x  广东深圳 电信            │ ← 后端取，截图自带网络画像
│  测试：XXX游戏 · 区服3   （可选，来自链接参数）│
├─────────────────────────────────────────────┤
│ 分组      节点          状态      耗时(ms)   │
│ 基线-国际  Google       ✅成功    120        │
│ 基线-国内  baidu        ✅成功    35         │
│ 登录      SDK检测       ✅成功    42         │
│ CDN       资源更新列表  ✅成功    88         │
│ 区服      区服列表(9001)⏱超时     5000       │
├─────────────────────────────────────────────┤
│ 结论：区服列表超时，建议切换网络/VPN（可选） │
└─────────────────────────────────────────────┘
```

### 5.2 玩家页流程（状态机）

```
1. 页面加载 → GET /api/session?g=&s=&t=
   后端: 签发 session_id → 取真实玩家IP(注意XFF链路) → ip2region/MaxMind富化geo
        → 写 ClickHouse tl_session(此时已有记录) → 返回 {session_id, player, targets[]}
2. 页面渲染顶部(编号/IP/归属地/标签) + 表格骨架
3. 对每个 target 探测:
   - 按 mode 决定 fetch cors / no-cors
   - repeat_count 次, cache_bust 时加 ?_t=<rand>
   - AbortController 控制 timeout_ms
   - cors 目标额外读 performance.getEntriesByName(url) 填 dns/tcp/tls/ttfb(非0即TAO存在)
4. 每个 target 完成 → POST /api/report(该 target 的 attempts[]) → 后端写 ClickHouse
5. 全部完成 → POST /api/report?final=true → 后端计算判定矩阵 → 返回 verdict → 页面显示结论
6. 页面卸载 → navigator.sendBeacon('/api/report', finalPayload) 兜底
```

### 5.3 链接上下文标签（低成本高价值）

客服生成 `https://test.xxx.com/?g=游戏ID&s=区服&t=工单号` 发给玩家，页面顶部显示「测试：XXX 区服3」，截图自带语义，后台按工单/游戏筛选。

### 5.4 延迟测量注意

- **CDN 缓存目标必须 cache_bust**：`versionmap.txt` 在 CF 会被缓存，不加 `?_t=` 会测到缓存命中（延迟≈0，假阳性）。
- **冷连接延迟**：同 host 重复 fetch 复用 TCP/TLS，4 次平均只有第 1 次含完整握手。沿用 4 次平均求稳，**额外单独记 attempt_no=1 为「冷连接延迟」**，更反映玩家真实首次接入。
- **连接复用**：不同 host 的目标互相独立，并发探测无干扰。

---

## 6. 判定矩阵

### 6.1 基线 vs 业务

目标按 **role** 分为两类：
- `基线`：第三方高可用站点，用于判断玩家自身网络是否正常
- `业务`：我方 API，出问题即为实际故障

### 6.2 判定规则

| 基线 | 业务 | verdict_code | 结论 |
|---|---|---|---|
| 全❌ | - | `PLAYER_NET_DOWN` | 玩家自身网络异常 |
| ✅ | 全❌ 且业务目标均已完成上报 | `OUR_API_DOWN` | 我方 API 全部不可达 |
| ✅ | 部分❌ | `PARTIAL_FAIL` | 部分模块异常 |
| ✅ | 可达但超过延迟阈值 | `PARTIAL_FAIL` | 部分模块延迟较高 |
| ✅ | 目标未完成上报 | `INCONCLUSIVE` | 数据不完整，避免误判全部故障 |
| ✅ | 全✅且未超过延迟阈值 | `ALL_OK` | 当前网络连接测试正常 |

- 任一基线 reachable 即视为基线通（冗余避免单点误判）
- 业务目标使用 session 创建时保存的 `config_snapshot` 作为期望目标列表，玩家提前关页导致目标未上报时不会被当成“业务全挂”
- `latency_warn_ms > 0` 时，使用暖连接平均延迟（第 2 次及以后；若只有 1 次则用第 1 次）判断是否偏慢
- 判定在 Go `internal/verdict` 模块服务端计算，玩家页同步显示

### 6.3 路径覆盖诊断（你给的三个接口恰好覆盖三条路径）

| 目标 | 路径性质 | 测什么 |
|---|---|---|
| `CDN 静态文件` | CF CDN 静态 | CDN 边缘路径可达性 |
| `api.example.com/v1/ip` | SDK API（CORS） | SDK 路径 + 返回 SDK 所见玩家出口 IP |
| `游戏区服列表(9001端口)` | 源站 PHP，9001 端口 | 源站 IDC 直连 + 非标端口可达性 |

不同路径组合失败指向不同故障域。例：CF 通 + 9001 超时 = 源站直连/端口问题，非 CDN 问题。

---

## 7. 数据模型

### 7.1 `tl_target`（ClickHouse — 目标配置，后台 CRUD）

```sql
CREATE TABLE tl_target (
    id              UInt64,
    name            String,
    group_name      String,        -- 登录/CDN/区服/基线-国内/基线-国际
    role            String,        -- 基线/业务，兼容旧 baseline/business
    url             String,        -- 含端口/查询参数
    method          String,        -- 默认 GET
    mode            String,        -- cors/no-cors
    timeout_ms      UInt32,        -- 默认 5000
    repeat_count    UInt8,         -- 默认 4
    cache_bust      UInt8,         -- 默认 0, CDN 静态目标置 1
    latency_warn_ms UInt32,        -- 延迟阈值，0=不检查
    extract_rule    String,        -- JSON, 可空, cors 目标从响应体提取展示字段
    player_visible  UInt8,         -- 默认 1
    display_order   UInt32,
    enabled         UInt8,         -- 默认 1
    note            String,
    updated_at      DateTime,
    version         UInt64         -- 递增, ReplacingMergeTree 取最新
) ENGINE = ReplacingMergeTree(version)
  ORDER BY id;
```

- **CRUD（无 mutation）**：新增/修改 = INSERT 同 id 的新版本行（version+1）；删除 = INSERT `enabled=0` 新版本。
- **读取**：`/api/session` 每次都读，建议 Go 内存缓存 + 后台改配置时刷新，或查询带 `FINAL`。表极小，无压力。

### 7.2 `tl_session`（ClickHouse — 一次玩家访问，append + ReplacingMergeTree）

```sql
CREATE TABLE tl_session (
    session_id      String,        -- MMDD-XXXXXX, 后端签发, 冲突重生成
    created_at      DateTime,      -- 首次创建时间, 后续版本保持不变
    player_ip       String,        -- 后端取的真实 IP(注意 XFF 链路)
    country         String,        -- GeoIP 富化
    province        String,
    city            String,
    isp             String,
    asn             String,
    ua              String,
    browser         String,        -- 解析自 UA
    os              String,
    net_type        String,        -- navigator.connection: 4g/wifi/unknown
    net_downlink    String,        -- downlink Mbps
    game            String,        -- 标签 ?g
    server          String,        -- 标签 ?s
    ticket          String,        -- 标签 ?t
    symptom         String,        -- 玩家自报症状(可选)
    note            String,        -- 客服/DevOps 后续补的备注(可变)
    verdict         String,        -- 判定 code
    verdict_detail  String,        -- 判定详情(如失败目标列表)
    config_snapshot String,        -- JSON, 本次测试目标列表快照
    version         UInt64         -- 递增, 更新(verdict/note/symptom)插新版本
) ENGINE = ReplacingMergeTree(version)
  ORDER BY session_id;
```

- **更新即追加**：final 上报写 verdict、admin 补 note/symptom → INSERT 同 session_id 新版本行（version+1，created_at 沿用原值）。无 mutation。
- **点查（按编号，主入口）**：`SELECT ... FROM tl_session WHERE session_id=? ORDER BY version DESC LIMIT 1`。
- **列表/搜索**：用 `argMax` 取每会话最新版本：
  ```sql
  SELECT session_id,
         argMax(player_ip,version) ip, argMax(isp,version) isp,
         argMax(verdict,version) verdict, max(created_at) created_at
  FROM tl_session
  WHERE created_at >= ? AND isp = ?
  GROUP BY session_id;
  ```
- **唯一性**：ClickHouse 无 UNIQUE 约束，靠 `MMDD + 6 位 base32`（~10^9/天空间）+ 插入前点查存在性，冲突重生成。
- 搜索维度（isp/province/game/ticket/time）直接 WHERE 扫描，本量级足够；后续可加 projection 优化。

### 7.3 `tl_probe_result`（ClickHouse — 探测事件，geo 反范式冗余）

```sql
CREATE TABLE tl_probe_result (
    session_id     String,
    target_id      UInt64,
    target_name    String,          -- 冗余, 免 join 展示
    group_name     String,          -- 冗余, UI 分组
    role           String,          -- '基线'/'业务'，兼容旧 baseline/business
    url            String,
    host           String,
    port           UInt16,
    attempt_no     UInt8,
    outcome        String,          -- reachable/timeout/fast_fail/http_error
    http_status    Nullable(UInt16),
    total_ms       UInt16,
    cold_ms        Nullable(UInt16),-- attempt_no=1 的冷连接延迟
    dns_ms         Nullable(UInt16),
    tcp_ms         Nullable(UInt16),
    tls_ms         Nullable(UInt16),
    ttfb_ms        Nullable(UInt16),
    resolved_ip    Nullable(String),-- 浏览器无法提供真实解析 IP，当前通常为空
    resp_headers   String,
    resp_body      String,
    resolved_geo   Nullable(String),
    error          String,
    created_at     DateTime,
    -- 冗余 session 上下文, 供多维聚合免 join
    player_ip      String,
    country        String,
    province       String,
    city           String,
    isp            String,
    asn            String,
    game           String,
    server         String
) ENGINE = ReplacingMergeTree(created_at)
  ORDER BY (session_id, target_id, attempt_no);
```

- `ReplacingMergeTree` 按 `(session_id, target_id, attempt_no)` 去重，吸收流式上报/sendBeacon 重复。
- `ORDER BY` 服务于「按编号点查」主入口；多维聚合查询在此量级直接扫描即可，若变慢（Phase 2）加 projection 或物化视图。

### 7.4 典型分析查询（ClickHouse）

```sql
-- 按目标成功率
SELECT target_name,
       countIf(outcome='reachable') / count() AS ok_rate,
       quantile(0.95)(total_ms) AS p95
FROM tl_probe_result
WHERE created_at >= ?
GROUP BY target_name ORDER BY ok_rate;

-- 运营商 × 地区 × 目标 三维交叉（定位区域性故障）
SELECT isp, province, target_name,
       countIf(outcome='reachable') / count() AS ok_rate,
       quantile(0.5)(total_ms) AS p50
FROM tl_probe_result
WHERE created_at >= ? AND role='business'
GROUP BY isp, province, target_name
HAVING ok_rate < 0.9
ORDER BY ok_rate;

-- 超时 vs 快速失败 分布
SELECT target_name, outcome, count()
FROM tl_probe_result
WHERE created_at >= ?
GROUP BY target_name, outcome;
```

---

## 8. API 接口清单

### 8.1 玩家侧

**`GET /api/session`**（页面加载，签发编号 + 返回目标列表）

- Query: `g` `s` `t`（可选标签）
- 后端：签发 `session_id`、取 IP、GeoIP 富化、写 `tl_session`、读 enabled 且 player_visible 的 targets
- Response:
```json
{
  "session_id": "0622-K7QX9P",
  "player": {"ip":"120.x.x.x","country":"中国","province":"广东","city":"深圳","isp":"电信","asn":"4134"},
  "targets": [
    {"id":1,"name":"Google","group_name":"基线-国际","role":"基线","url":"https://www.google.com/generate_204","mode":"no-cors","timeout_ms":5000,"repeat_count":2,"cache_bust":0,"latency_warn_ms":0},
    {"id":7,"name":"区服列表","group_name":"区服","role":"业务","url":"https://game.example.com:9001/index.php/server/simplelists?...","mode":"no-cors","timeout_ms":5000,"repeat_count":4,"cache_bust":0,"latency_warn_ms":3000}
  ]
}
```

**`POST /api/report`**（流式上报，可多次调用）

- Body:
```json
{
  "session_id": "0622-K7QX9P",
  "net_type": "4g",
  "net_downlink": "1.5",
  "results": [
    {"target_id":7,"attempt_no":1,"outcome":"timeout","http_status":null,"total_ms":5000,"cold_ms":5000,"dns_ms":null,"tcp_ms":null,"tls_ms":null,"ttfb_ms":null,"error":"aborted"}
  ]
}
```
- `?final=true`：标记本次为最后一批，触发服务端计算 verdict 并返回：
```json
{"verdict":"PARTIAL_FAIL","verdict_detail":"部分模块异常: 区服列表；延迟较高: SDK 800ms>500ms"}
```
- 后端：校验 target_id ∈ enabled targets（白名单）→ 富化 geo（取自 session）→ 写 ClickHouse → final 时算 verdict 回写 tl_session（追加新版本行）。

**`POST /api/report`（sendBeacon 兜底）**：同一端点，`Content-Type: text/plain`（beacon 限制），后端兼容解析。

### 8.2 后台侧（Admin，需鉴权）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/targets` | 目标列表 |
| POST | `/admin/targets` | 新增目标 |
| PUT | `/admin/targets/:id` | 修改 |
| DELETE | `/admin/targets/:id` | 删除（软删 enabled=0） |
| GET | `/admin/sessions` | 多维搜索（ip/isp/province/game/ticket/time，分页） |
| GET | `/admin/sessions/:id` | 详情：session + 全部 probe_result |
| PATCH | `/admin/sessions/:id` | 补 note/symptom |

---

## 9. 限流与安全

### 9.1 限流（Redis，可配置）

| 维度 | 默认 | key |
|---|---|---|
| 单 IP 创建 session | 5 次/分钟 | `rl:session:{ip}` |
| 单 IP 上报 | 60 次/分钟 | `rl:report:{ip}` |
| 全局 session | 100/秒（保护） | `rl:global:session` |
| 超限 | HTTP 429 | |

- 当前实现为 Redis `INCR` + `EXPIRE` 固定窗口。
- Redis 不可用时自动放通，不阻塞业务。

### 9.2 安全

- **目标白名单**：玩家页只能测后端下发的 enabled 目标；`/api/report` 忽略未知 `target_id`，**玩家不可注入任意 URL**（防开放代理/放大攻击）。
- **payload 校验**：outcome ∈ 枚举、latency 有界、字段长度限制。
- **Admin 鉴权**：JWT（无状态，不落 DB）或静态 token，独立路径前缀 `/admin`。账号凭证放配置文件/env，不进 ClickHouse。
- **XFF 链路**：当前由 Gin `ClientIP()` 根据 `trusted_proxy_cidrs`（优先）或旧的 `trusted_proxies` 配置取客户端 IP；生产反代部署时必须填写反代/负载均衡 CIDR，避免把代理 IP 或伪造 XFF 当成玩家 IP。
- **HTTPS + HSTS**；tags 输入消毒；ClickHouse 账号限表。
- **账号信息**：拨测 URL 中的真实账号（如 `account=demo`）建议后台配 dummy 账号专供拨测，避免公开页暴露。

---

## 10. GeoIP 方案

当前使用 **4 个离线库**，全部本地存储、启动加载：

| 库 | 文件 | 用途 | Go 读取 |
|---|---|---|---|
| **ip2region IPv4** | `ip2region_v4.xdb` | 中国大陆省/市/ISP | `github.com/lionsoul2014/ip2region/binding/golang/xdb` |
| **ip2region IPv6** | `ip2region_v6.xdb` | IPv6 的中国大陆省/市/ISP | 同上 |
| **MaxMind Country** | `GeoLite2-Country.mmdb` | 港澳台/国际国家或地区 | `github.com/IncSW/geoip2` |
| **MaxMind ASN** | `GeoLite2-ASN.mmdb` | ASN/运营商组织 | `github.com/IncSW/geoip2` |

### 融合查询逻辑（session 富化时）

1. 先查 ip2region，保留中国大陆省/市/ISP 粒度。
2. 同时查 MaxMind Country/ASN 做国际和港澳台补充。
3. 若 ip2region 返回“中国”但 MaxMind 返回 Taiwan/Hong Kong/Macau 等地区，优先采用 MaxMind 的 country/ASN，避免港澳台被粗粒度归为中国大陆。
4. 合并填入 `country/province/city/isp/asn`。session 创建时同步富化。

### 更新

ip2region 不定期更新（关注 repo release）；MaxMind GeoLite2 每周/月更新。建议定期拉取替换 + 重启服务（或后续加热加载）。

### 交叉校验（Phase 2，可选）

cors 基线目标（ipinfo/ip.sb）返回的 geo 与本地 GeoIP 比对，不一致 → 标记疑似代理/VPN。不主走第三方 API：避免限流 + 玩家 IP 外泄 + 外部依赖。

---

## 11. 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go + Gin |
| 数据库 | ClickHouse（ReplacingMergeTree，配置/session/事件统一） |
| 限流/计数 | Redis |
| 鉴权 | JWT / 静态 token（无状态，不落 DB） |
| GeoIP | ip2region + MaxMind GeoLite2（或 DB-IP lite） |
| 玩家页 | 单 HTML（配置驱动 + 编号 + 流式上报 + i18n） |
| 后台前端 | 单 HTML（目标 CRUD + 记录查询/详情） |
| 部署 | Docker / 单 Go 二进制 + nginx（TLS/静态） |

---

## 12. 项目骨架与部署

> v1.0 已实现，`go test ./...` + `go vet ./...` 通过。

```
Test Link/
├── go.mod / go.sum               # Go 模块依赖
├── config.yaml                   # 应用配置（端口/ClickHouse/Redis/限流/GeoIP路径/鉴权）
├── docker-compose.yml            # ClickHouse + Redis 本地开发环境
├── DESIGN.md                     # 本设计文档
├── index.html                    # 旧版（保留参考）
├── ip2region_v{4,6}.xdb          # GeoIP 国内数据
├── GeoLite2-{ASN,Country}.mmdb   # GeoIP 国际数据
├── cmd/server/
│   ├── main.go                   # 入口：gin 路由、优雅关停
│   └── config.go                 # YAML 配置加载（带默认值）
├── internal/
│   ├── model/model.go            # 共享类型（Session/Target/ProbeResult/API DTOs）
│   ├── geoip/geoip.go            # ip2region + MaxMind 离线 GeoIP
│   ├── store/clickhouse.go       # ClickHouse 连接 + DDL + CRUD + 种子数据
│   ├── session/service.go        # Session 管理（编号签发/geo富化/UA解析/verdict更新）
│   ├── target/service.go         # 目标配置 CRUD（追加版本、软删除）
│   ├── probe/service.go          # 上报处理、config snapshot 校验、裁决触发
│   ├── verdict/verdict.go        # 判定矩阵（snapshot 完整性 + 延迟阈值）
│   ├── ratelimit/ratelimit.go    # Redis 固定窗口限流（容错放通）
│   ├── auth/auth.go              # JWT + 静态 token 双模式鉴权
│   └── api/
│       ├── handler.go            # HTTP handlers（玩家 2 个 + 后台 8 个）
│       └── middleware.go         # 限流中间件 + admin 鉴权中间件
├── web/
│   ├── player/index.html         # 玩家测试页（配置驱动、session编号、流式上报、结论、i18n）
│   └── admin/index.html          # 管理后台（目标CRUD、按编号查询、多维搜索、详情）
└── migrations/
    └── 001_init.sql              # ClickHouse DDL 参考（应用启动时自动执行）
```

- 开发：`docker-compose up -d` 起 ClickHouse + Redis，`go run ./cmd/server/ config.yaml`。
- 生产：Docker 镜像 `whpan/testlink:latest` 或 Go 二进制 + nginx 反代，ClickHouse/Redis 用现有实例。
- ClickHouse 表 DDL 在应用启动时自动执行（`store.Init`），也保留 `migrations/` 为参考。
- GeoIP 库文件置项目根目录（`config.yaml` 中 `geoip.*` 路径指向），可 cron 更新后重启服务（或后续加热加载）。

---

## 13. 分期计划

### Phase 1（MVP）
- `GET /api/session` + `POST /api/report`（含 final/verdict）
- 玩家页：配置驱动、测试编号、IP/归属地展示、流式上报、结论行
- GeoIP 富化（ip2region + MaxMind/DB-IP）
- ClickHouse（config + session + probe_result，统一 ReplacingMergeTree）
- 后台：目标 CRUD、按编号查询、多维搜索、基础看板（成功率/延迟按目标）
- 限流（Redis）+ 白名单 + 鉴权

### Phase 2（深度分析）
- 完整分析看板：运营商×地区×目标 三维交叉、p50/p95/p99 趋势、超时/快速失败分布
- 判定矩阵看板 + 故障率突增告警
- 代理/VPN 检测（geo 交叉校验）
- note/symptom 工作流、工单关联
- ClickHouse projection/物化视图优化

### Phase 3（可选）
- DoH 解析 IP 上下文
- 存储分层（原始事件 N 月 + 按天聚合永久）
- 人机校验加固（若被刷）
- 原生探针（用户已表示不需要，仅留口）

---

## 14. 后续可选项

| 项 | 取舍 |
|---|---|
| DoH 解析 IP | 对照组已覆盖防误判需求，Phase 1 不做，需要再加 |
| 第三方 API 做 geo | 不主走（限流+隐私+依赖），仅 cors 基线附带 |
| Kafka 缓冲 | 量级未到，直接写 ClickHouse，到百万/分钟再上 |
| 原生探针（ICMP/UDP/traceroute） | 用户已确认不做 |
| InfluxDB/Prometheus | 不需要，ClickHouse 已覆盖 |

---

## 当前状态

**v0.1 已交付** — Go 后端 + ClickHouse DDL + 玩家页 + 后台，全部可编译运行。下一步试跑功能，根据反馈迭代。

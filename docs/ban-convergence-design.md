# 封禁架构收敛设计 v2 —— 单向 Kick 模型

> 决议定稿 2026-10。核心原则：**封禁操作与数据 100% 收敛 loginserver**（MySQL 权威 +
> Redis 集合与会话缓存）；sgate 退化为纯执行器（暴露 Kick gRPC，本地查表踢连接）；
> **全程单向**：sgate 不反向调用 loginserver，不存封禁，不存 loginKey。

## 1. 架构原则

1. 封禁**只在 loginserver 写**（HTTP API → MySQL + Redis），sgate 无任何封禁存储与判定
2. sgate 不主动上报会话；session 缓存由 **loginserver 在 Login HTTP 成功时同步写入**
3. 踢人是**单向 RPC**：loginserver → sgate（Kick），sgate 本地查表执行，不落任何数据
4. 踢人失败**不回滚封禁**：权威库先写、踢 best-effort，失败仅告警+计数
5. sgate 删除全部 admin 封禁端点，运维入口唯一

## 2. 会话缓存（Redis，仅 loginserver 读写）

Login HTTP 控制器在校验通过、签发 JWT 后**同步写**（成功响应前；sgate 是否最终收到
LoginOK 不影响本缓存——它只是踢人路由索引，不是在线凭证）：

| 键 | 类型 | 值/成员 | TTL |
|---|---|---|---|
| `sess:conn:{sgateId}:{connId}` | hash | `{account_id, user_id, role_id, ip, login_at}` | 24h |
| `sess:acct:{accountId}` | set | 成员 `{sgateId}:{connId}` | 24h |
| `sess:ip:{ip}` | set | 成员 `{sgateId}:{connId}` | 24h |

- `sgateId`/`connId` 来自 sgate 签发的 JWT claims（Login HTTP 请求头 `X-Sgate-Conn`）；
  claims 缺失则跳过写缓存（不阻断登录）
- **无 Unbind**：正常下线不清键，靠 TTL 收敛；死条目踢人时由 sgate 本地 no-op
- 无 TTL 刷新：>24h 在线会话踢不中（登录校验兜底），见决议 11

## 3. 核心流程

### 3.1 封禁（HTTP，支持批量）

```
POST /admin/bans (BannedRole) | /admin/blacklist-ip  ← 批量 payload
1. 写 MySQL（权威，upsert）
2. 写 Redis banned 集合（登录校验用）  ← 权威库先落，无条件成功
3. best-effort 踢人（默认同步，2s 超时）：
   a. 对每个 accountId：SMEMBERS sess:acct → 解析 sgateId 去重
      对每个 ip：SMEMBERS sess:ip → 解析 sgateId 去重
   b. 对涉及的每台 sgate（池内连接）：
      KickAccounts([accountId...]) / KickIP(ip)
   c. sgate 本地：accountId → connIDToAccountID 反查逐个 kickConn；
      ip → conn 表扫 RemoteHost 全踢
   d. 失败仅计数告警；响应体带 kicked/missed 计数
4. 返回（含踢中/未中统计）
```

**落库顺序必须是 1→2→3**：封禁是权威操作，不能依赖踢人成功（sgate 不可达/进程崩溃
时封禁仍生效；被踢漏的人重登会被登录校验拦截）。

### 3.2 解封（HTTP）

删 MySQL 行 + 删 banned 集合成员；**不**做任何会话动作（被踢的人重登自然恢复）。

### 3.3 登录校验（已有，不变）

AddBan 时 sgate rejectIfBanned 槽位已删；sgate 不再存封禁。校验发生在两处：
- **Login HTTP**（loginserver，查 Redis banned 集合 + MySQL）——权威拦截
- **gRPC ValidateLoginToken**（每角色上线前，sgate 调）——读 MySQL ACL 内存快照，
  ban 表纳入快照（gen 轮询）

### 3.4 已知竞态（接受，P2 收口）

Login HTTP 成功 → 玩家过 sgate 登录门之间，若发生封禁：踢人 no-op（尚未连接），
且玩家持有 ban 前 JWT 可过门上线。窗口极小；P2 用 `BatchValidateLoginTokens`
周期回刷 sgate 本地校验缓存彻底收口（sgate 届时存 loginKey，仅用于该校验）。

## 4. 协议变更（protocol）

### sgate.proto（新增 Kick 服务，loginserver→sgate 单向）

```protobuf
service KickService {
  rpc KickAccounts(KickAccountsRequest) returns (KickAccountsResponse);
  rpc KickIP(KickIPRequest) returns (KickIPResponse);
}
```

- `KickAccountsRequest{repeated int64 account_ids}`；响应 `kicked_count`
- `KickIPRequest{string ip}`；响应 `kicked_count`（该 IP 在本 sgate 的全部连接）
- 服务端鉴权：`x-internal-token`（`ugrpc.TokenAuthServerInterceptor`，sgate 配置校验）

### loginserver.proto

**无变更**（不需要 Bind/Unbind，v1 方案中的两条 RPC 废弃）。

## 5. 客户端与依赖

- **SgateClientPool**（loginserver）：`ugrpc.Pool[protoKick.KickClient]`，
  源为 SGATE:{zone} 的 etcd Discovery（sgate 需自注册，地址参数按 logic 模式）
- **sgate 自注册 etcd**：唯一遗留的 sgate 侧集群改动；无其他 sgate→loginserver 依赖
- token：单一 `x-internal-token`（loginserver 拨号携带 / sgate 校验），无 bind token

## 6. 代码修改清单

### sgate

| 动作 | 内容 |
|---|---|
| 🗑 删 | `admin_ban.go` 全部端点；`banStore`、`findBan`、`banMatches`、`rejectIfBanned`、`enforceBan*`；`security.BanStore`；banRedis 配置与测试 |
| ✂️ 抽 | 踢人动作抽 `kickConn(conn, reason, code)`（通知→撤 JWT→notifyLogicOffline→close），登出/维护/Kick RPC 共用 |
| ➕ 增 | gRPC `KickService`：`KickAccounts`（connIDToAccountID 反查）、`KickIP`（conn 表扫 RemoteHost）；`x-internal-token` 拦截器注入 |
| ✏️ 改 | `cluster_component.go`：开启自注册 etcd（instanceId/AdvertiseAddr=OutboundIPv4+port/LeaseTTL 10s，照 logic 模式） |
| ✏️ 改 | config：`kickInternalToken` |
| ➕ 增 | README/运维文档：封禁操作唯一入口 loginserver |

### loginserver

| 动作 | 内容 |
|---|---|
| ➕ 增 | `rmodel/repo_session.go`：Login 时写三键（§2）；封禁时 SMEMBERS 解析 sgateId |
| ✏️ 改 | `LoginController`：签发 JWT 后同步写 session 缓存（claims 缺 sgate/conn 则跳过） |
| ➕ 增 | `SgateClientPool`（util `ugrpc.Pool`）+ etcd SGATE Discovery |
| ✏️ 改 | `service_acl` 写路径挂 `kickAfterBan`：**先落库（现有写路径），再同步 best-effort 踢（2s 超时），响应附 kicked/missed 计数** |
| ✏️ 改 | config：`sgate.internalToken` |

### protocol

`sgate.proto`（或新 `kick.proto`）新增 KickService + regen；`loginserver.proto` 不动。

### util

无新增（Pool/TokenAuth/TokenDialOptions 已落地）。`PoolSource` 已随 util 解耦 V5 从
uetcd 移除：sgate 适配（ServiceEvent→SourceEvent 转换）由 loginserver 在
SgateClientPool 接线时以本地小适配器实现（~10 行，见
`E:\util\docs\package-decoupling-design.md` §4 V5）。

## 7. 实施步骤（P1）

1. protocol：KickService + regen + 门禁
2. loginserver：repo_session 三键写入 + LoginController 挂点 + 单测
3. loginserver：SgateClientPool + SGATE Discovery + 单测
4. loginserver：kickAfterBan（先库后踢、超时、计数）挂入 ACL 写路径
5. sgate：kickConn 抽取 + KickService（含 token 拦截器）+ 自注册 etcd
6. sgate：删除封禁存储/判定/端点 + 清理测试
7. 三仓门禁（gofmt/build/vet/staticcheck/test/go fix）
8. drill：批量封禁在线账号→秒踢（响应带计数）；封 IP→该 IP 全踢；
   封禁→立即重登被拦截；解封→恢复；sgate 不可达→封禁仍落库、计数告警；
   Login 后立即封禁（mid-login）→记录竞态行为符合 §3.4 预期

P2（后续）：`BatchValidateLoginTokens` 周期安全网（收口 §3.4 竞态 + TTL 刷新）；
反代场景 sgate trustedProxies；util 解耦 P0–P4 并行推进。

## 8. 决议记录（2026-10 定稿）

| # | 问题 | 决议 |
|---|---|---|
| 1 | TTL 临时封禁 | 砍掉，仅永久封禁入权威库 |
| 2 | sgate admin 端点 | 直接删除，文档写迁移指引 |
| 3 | 离线 JWT 撤销 | 登录校验兜底；§3.4 mid-login 竞态接受、P2 BatchValidate 收口 |
| 4 | sgate etcd 注册 | 开启自注册（Kick 路由需要 live 地址） |
| 5 | 会话缓存写入方 | **loginserver Login HTTP 同步写**（v1 的 sgate Bind 上报废弃） |
| 6 | Kick RPC 鉴权 | `x-internal-token`，sgate 配置校验 |
| 7 | bind token | **不存在**（无 Bind RPC，v1 决议 7 废弃） |
| 8 | 踢人时序 | **先落权威库、再 best-effort 踢**（2s 超时，失败不回滚），响应附计数 |
| 9 | IP 踢人口径 | sgate 本地 RemoteHost 全扫；反代 XFF→trustedProxies 记 P2 |
| 10 | 存量 banRedis | 默认无迁移；有在封数据再单独确认 |
| 11 | 注册表 TTL=24h | 接受超长在线踢不中；P2 回刷 |
| 12 | 总体模型 | **单向 Kick 模型**（本 v2）：session 登录时写、sgate 纯执行器、无反向 RPC |

# 封禁架构收敛设计 v2.1 —— 单向 Kick 模型

> 决议定稿 2026-10。核心原则：**封禁操作与数据 100% 收敛 loginserver**（MySQL 权威 +
> Redis 集合与会话缓存）；sgate 退化为纯执行器（暴露 Kick gRPC，本地查表踢连接）；
> **全程单向**：sgate 不反向调用 loginserver 业务 API，不存封禁，不存 loginKey。
>
> **v2.1 健壮性增强**（2026-10-16）：①Session Unbind（OnClose 清理会话缓存，删 TTL
> 死条目）②CIDR 封禁精确路由（sess:ip24 前缀索引）③Kick notify 可靠投递
> （SendThenClose 保证客户端收到 KickNotify 后再断连）④反代真实 IP
> （WS trustedProxies + TCP PROXY protocol v1）⑤admin 会话查询 API。
> 详见 §9。

## 1. 架构原则

1. 封禁**只在 loginserver 写**（HTTP API → MySQL + Redis），sgate 无任何封禁存储与判定
2. sgate 不主动上报会话；session 缓存由 **loginserver 在 ValidateLoginToken gRPC 校验
   通过时写入**（sgate 过门校验携带 `sgate_id/conn_id/client_ip`；HTTP /api/v1/login
   是预铸 loginToken 的工具接口，不写会话缓存）
3. 踢人是**单向 RPC**：loginserver → sgate（Kick），sgate 本地查表执行，不落任何数据
4. 踢人失败**不回滚封禁**：权威库先写、踢 best-effort，失败仅告警+计数
5. sgate 删除全部 admin 封禁端点，运维入口唯一

## 2. 会话缓存（Redis，仅 loginserver 读写）

**写入点**：sgate LoginGate 过门校验调用 gRPC `ValidateLoginToken` 时在请求中携带
`sgate_id/conn_id/client_ip`；loginserver 校验通过后同步写三键（best-effort，写失败
仅告警不阻断校验；BatchValidate 复用同消息但不带上下文，天然跳过）。
sgate 是否最终收到 LoginOK 不影响本缓存——它只是踢人路由索引，不是在线凭证：

| 键 | 类型 | 值/成员 | TTL |
|---|---|---|---|
| `sess:conn:{sgateId}:{connId}` | hash | `{account_id, ip, login_at}` | 24h |
| `sess:acct:{accountId}` | set | 成员 `{sgateId}:{connId}` | 24h |
| `sess:ip:{ip}` | set | 成员 `{sgateId}:{connId}` | 24h |
| `sess:ip24:{a.b.c.0/24}` | set | 成员 `{sgateId}:{connId}` | 24h |

- `sgateId`/`connId`/`clientIp` 由 sgate 在 ValidateLoginTokenReq 中携带；
  `sgate_id/conn_id` 任一为空则跳过写缓存（不阻断校验）
- `sess:ip24` 仅 IPv4 写入（CIDR 踢人路由用，v2.1 §9.2）；IPv6 无该键
- **Session Unbind（v2.1 §9.1）**：sgate OnClose 异步调 gRPC `RemoveSession`
  （best-effort，2s 超时，失败仅告警）删 conn hash + SREM 三集合成员；
  TTL 仍保留 24h 作为兜底（sgate 崩溃/网络分区时收敛死条目）
- 无 TTL 刷新：>24h 在线会话踢不中（登录校验 + BatchValidate 兜底），见决议 11

## 3. 核心流程

### 3.1 封禁（HTTP，支持批量）

```
POST /admin/bans (BannedRole) | /admin/blacklist-ip  ← 批量 payload（纯 IP 或 CIDR）
1. 写 MySQL（权威，upsert）
2. 写 Redis banned 集合（登录校验用）  ← 权威库先落，无条件成功
3. best-effort 踢人（默认同步，2s 超时）：
   a. 纯 IP：SMEMBERS sess:ip:{exact} → 解析 sgateId 去重
      CIDR：SessionSgatesByCIDR（/24 及更小展开 sess:ip24 前缀键并集，
            /23 及更大或 IPv6 → needAll=true fallback 全部已知 sgate）
      accountId：SMEMBERS sess:acct → 解析 sgateId 去重
   b. 对涉及的每台 sgate（池内连接）：
      KickAccounts([accountId...]) / KickIP(ip)（ip 可为 CIDR，sgate 本地前缀匹配）
   c. sgate 本地：accountId → connIDToAccountID 反查逐个 kickConn；
      ip → conn 表前缀/精确匹配（KickByIP CIDR 用 netip.Contains）
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

### 3.4 已知竞态（P2 已收口 2026-10-16）

Login HTTP 成功 → 玩家过 sgate 登录门之间，若发生封禁：踢人 no-op（尚未连接），
且玩家持有 ban 前 JWT 可过门上线。窗口极小；P2 已用 `BatchValidateLoginTokens`
周期安全网收口（sgate 登录后留存 loginKey，每 batchInterval 批量重校验全部在线会话，
失效/被封禁的本地踢下线；同时兜底会话缓存 TTL=24h 过期踢不中）。

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

- **v2.1 新增** `RemoveSession(RemoveSessionReq) returns (RemoveSessionAck)`：
  sgate OnClose 异步调用，清理会话缓存三键 + ip24（best-effort，幂等）
  `RemoveSessionReq{sgate_id, conn_id}`
- Bind RPC 仍不存在（sgate 不反向绑定；session 由 ValidateLoginToken 写入）

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

## 7. 实施步骤（P1，已完成 2026-10-15）

1. protocol：KickService + ValidateLoginTokenReq 三字段 + regen + 门禁 ✅
2. loginserver：repo_session 三键写入 + ValidateLoginToken 挂点 ✅（HTTP 控制器不写）
3. loginserver：SgateClientPool（ugrpc.Pool + JSON 地址本地适配器）+ SGATE Discovery ✅
4. loginserver：kickAfterBan（先库后踢、超时、计数）挂 AddBlackIP ✅
5. sgate：kickConn 抽取 + KickService（token 校验）+ accountID 索引 + Validate 携带会话上下文 ✅
   （sgate etcd 自注册原本已开启，无需新增）
6. sgate：删除封禁存储/判定/端点 + 清理测试 ✅（BanStore/banRedis/rejectIfBanned/admin 端点全删）
7. 三仓门禁（gofmt/build/vet/staticcheck/test/go fix）✅
8. drill：批量封禁在线账号→秒踢（响应带计数）；封 IP→该 IP 全踢；
   封禁→立即重登被拦截；解封→恢复；sgate 不可达→封禁仍落库、计数告警；
   Login 后立即封禁（mid-login）→记录竞态行为符合 §3.4 预期
   ✅ drill 完成（2026-10-16，S1–S6 全 PASS）：
   - S1 账号封禁踢在线（reason=ban code=403）
   - S2 封禁后重登拦截（validation 查权威库拒）
   - S3 解封恢复（同 accountId 重登 LOGIN_OK）
   - S4 封 IP 双连接踢（reason=blacklist code=403，两连接均收 KICK_NOTIFY）
   - S5 sgate 停机封禁落库（ban 落库成功，best-effort 踢计数告警）
   - S6 mid-login 竞态：LOGIN_OK 后即收 KICK（符合 §3.4 接受预期）
   - drill 修正：KickAccounts.account_ids int64→string（accountId 为十六进制字符串）；
     AddBannedRole 补挂 kickAccountAfterBan；sgateDiscoveryWrapper 防 etcd 组件名撞名；
     sgateSource.ServiceSet 截取 etcd key 末段为 instanceID（与 Pool.Get 对齐）

P2 已完成（2026-10-16）：`BatchValidateLoginTokens` 周期安全网（收口 §3.4 竞态 +
会话缓存 TTL 过期兜底）——sgate 侧 `loginValidation.batchInterval`（默认 60s）/
`batchSize`（默认 200），登录留存 loginKey，周期批量校验踢失效会话。

v2.1 已完成（2026-10-16）：Session Unbind + CIDR 精确路由 + Kick notify 可靠投递 +
反代真实 IP（trustedProxies/PROXY protocol）+ admin 会话查询 API + BroadcastAll 优化
（§9.1–9.6）。util 解耦 P0–P5 已完成。

## 8. 设计简化分析（2026-10-16 评估）

**结论：现有"单向 Kick + ACL 快照 + 周期安全网"架构方向正确，无需大改。**
所有改进均为增量补强，不动核心模型。已评估的替代方案均被否决：

| 替代方案 | 否决理由 |
|---|---|
| sgate 反向拉取封禁（定期 pull） | 违背"封禁只在 loginserver 写"原则；引入 sgate 存储与一致性问题；现有 ACL 快照 gen 轮询已覆盖 |
| Redis Pub/Sub 广播踢人 | 不可靠（无 ack，sgate 短暂断连丢消息）；现有单向 RPC 有响应计数 |
| sgate 上报会话到 loginserver（push） | 信道复杂（心跳/TTL/重连），现有"loginserver 在校验时写"天然准确 |
| Bind/Unbind 双向 RPC | v1 已废弃；session 写入点在 ValidateLoginToken，反向绑定冗余 |
| 全局 Session 中心表（独立服务） | 现有 Redis 三键 + ip24 已满足路由需求；独立服务增加运维面，收益不成比例 |
| 关键消息可靠投递层（ack/重传） | Kick 是最终一致场景（登录校验 + BatchValidate 兜底），引入可靠性层复杂度不成比例 |

**增量改进优先级**（已全部落地或标注）：

| 改进 | 状态 | 价值 |
|---|---|---|
| Kick notify 可靠投递（SendThenClose） | ✅ v2.1 §9.3 | 保证客户端收到 KickNotify 后再断连 |
| Session Unbind（RemoveSession） | ✅ v2.1 §9.1 | 删 TTL 死条目，踢人路由更准 |
| CIDR 精确路由（sess:ip24） | ✅ v2.1 §9.2 | /24 及更小 CIDR 不再全量广播 |
| PROXY protocol（TCP 反代） | ✅ v2.1 §9.4 | TCP 场景取真实客户端 IP |
| admin 会话查询 API | ✅ v2.1 §9.5 | 运维可视化 |
| 关键消息可靠投递（通用层） | ❌ 推迟 | Kick 场景已被兜底覆盖，其他场景按需单独评估 |
| 全局 Session 表（独立服务） | ❌ 否决 | Redis 三键已够，见上表 |

## 9. v2.1 健壮性增强（2026-10-16 落地）

### 9.1 Session Unbind（RemoveSession RPC）

- **问题**：session 缓存无 Unbind，正常下线靠 TTL(24h) 收敛 → 踢人路由含死条目
- **方案**：sgate `OnClose` 异步调 loginserver `RemoveSession(sgate_id, conn_id)`，
  best-effort（2s 超时，失败仅告警，靠 TTL 兜底）
- **实现**：
  - protocol：`RemoveSessionReq/RemoveSessionAck` + rpc（loginserver.proto）
  - loginserver：`rmodel.RemoveSession`（HMGet 拿 acct/ip → pipeline Del+SREM
    acct/ip/ip24 成员）+ `LoginServer.RemoveSession` gRPC handler
  - sgate：`removeSessionAsync`（loginValidation 启用且有 accountID 时触发）
- **不变式**：sgate 仍不存封禁/loginKey；RemoveSession 仅是缓存维护，不是业务 RPC

### 9.2 CIDR 封禁精确路由（sess:ip24 索引）

- **问题**：CIDR 封禁无法用 `sess:ip:{exact}` 路由 → `kickIPAfterBan` 全量广播
- **方案**：写 session 时增写 `sess:ip24:{a.b.c.0/24}` 前缀键（仅 IPv4）；
  CIDR 踢人时 `SessionSgatesByCIDR` 展开覆盖的 /24 键并集去重路由
- **边界**：/24 及更小精确路由；/23 及更大或 IPv6 → `needAll=true` fallback 全量
- **sgate 侧**：`KickByIP` CIDR 用 `netip.Contains` 匹配（`parseKickPrefix`）

### 9.3 Kick notify 可靠投递（SendThenClose）

- **问题**：`kickConn` 先写 KickNotify 再 Close，异步写与 Close 竞态（Windows 无
  outbound flush）→ 客户端可能没收到通知就断连
- **方案**：`Connection.SendThenClose`（AsyncWritev 成功回调内 Close，保证数据
  发出后再关）；WS 走 EncodeWSFrame 分支
- **实现**：kickConn 优先 SendThenClose，失败回退同步 Close（不阻断踢人）

### 9.4 反代真实客户端 IP

- **WS（HTTP 握手）**：`SecurityConfig.TrustedProxies`（IP/CIDR 列表），
  命中则从 `X-Real-IP`（优先）/`X-Forwarded-For`（最左项）解析真实 IP，
  `UpdateConnectionIP` 重键 per-IP 计数 + 覆盖 RemoteHost 缓存
- **TCP（无 HTTP 头）**：`SecurityConfig.ProxyProtocol` 开关，
  首包 `PROXY ` 开头时解析 PROXY protocol v1（TCP4/TCP6/UNKNOWN，≤107 字节），
  同 `UpdateConnectionIP` 重键
- **注意**：trustedProxies/ProxyProtocol 均默认关闭；开启前需确认反代拓扑

### 9.5 admin 会话查询 API

- `GET /api/v1/sessions?limit=&sgateId=`：SCAN `sess:conn:*` 列表（默认 200，
  上限 500，sgateId 可选过滤），返回 `{sgateId, connId, accountId, ip, loginAt}`
- `GET /api/v1/sessions/account?accountId=`：账号 → 在线 sgate 反查
- 鉴权：`X-Admin-Token`；仅读，不改任何键

### 9.6 BroadcastAll 广播优化

- `BroadcastAll` 改快照 + 32 分片并发 Send（复用 kickConnectionsAsync pattern），
  仅 StateForward 入快照；`sync/atomic` import

## 10. 决议记录（2026-10 定稿，v2.1 增补）

| # | 问题 | 决议 |
|---|---|---|
| 1 | TTL 临时封禁 | 砍掉，仅永久封禁入权威库 |
| 2 | sgate admin 端点 | 直接删除，文档写迁移指引 |
| 3 | 离线 JWT 撤销 | 登录校验兜底；§3.4 mid-login 竞态 + TTL 过期已由 P2 BatchValidate 安全网收口 |
| 4 | sgate etcd 注册 | 开启自注册（Kick 路由需要 live 地址） |
| 5 | 会话缓存写入方 | **ValidateLoginToken gRPC 校验通过时写**（sgate 携带 sgate_id/conn_id/client_ip；与原设想的 Login HTTP 写入点不同——HTTP /api/v1/login 只是预铸 token 工具接口，真实过门走 gRPC） |
| 6 | Kick RPC 鉴权 | `x-internal-token`，sgate 配置校验 |
| 7 | bind token | **不存在**（无 Bind RPC，v1 决议 7 废弃） |
| 8 | 踢人时序 | **先落权威库、再 best-effort 踢**（2s 超时，失败不回滚），响应附计数 |
| 9 | IP 踢人口径 | sgate 本地 RemoteHost 全扫；反代 XFF→trustedProxies 记 P2 |
| 10 | 存量 banRedis | 默认无迁移；有在封数据再单独确认 |
| 11 | 注册表 TTL=24h | 超长在线踢不中由 P2 BatchValidate 安全网兜底（周期重校验踢失效会话） |
| 12 | 总体模型 | **单向 Kick 模型**（本 v2）：session 登录时写、sgate 纯执行器、无反向 RPC |
| 13 | Session Unbind | **v2.1 落地**：sgate OnClose 异步 RemoveSession（best-effort），TTL 保留兜底 |
| 14 | CIDR 路由 | **v2.1 落地**：sess:ip24 前缀索引精确路由；/23 及更大 fallback 全量 |
| 15 | Kick notify 投递 | **v2.1 落地**：SendThenClose 回调式关闭，保证客户端收到后再断连 |
| 16 | 反代真实 IP | **v2.1 落地**：WS trustedProxies + TCP PROXY protocol v1，默认关闭 |
| 17 | 关键消息可靠投递 | **推迟**：Kick 场景已被登录校验+BatchValidate 兜底覆盖，不引入通用可靠性层 |
| 18 | 全局 Session 表 | **否决**：Redis 三键+ip24 已满足路由，独立服务收益不成比例 |

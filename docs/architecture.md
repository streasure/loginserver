# 架构设计 —— 单向 Kick 模型

核心原则：**封禁操作与数据 100% 收敛 loginserver**（MySQL 权威 + Redis 会话索引）；
sgate 退化为纯执行器（暴露 Kick gRPC，本地查表踢连接）；**全程单向**：sgate 不反向
调用 loginserver，不存封禁，不存 loginKey。

## 1. 架构原则

1. 封禁**只在 loginserver 写**（HTTP Admin API → MySQL + 本地快照），sgate 无任何封禁存储与判定
2. sgate 不主动上报会话；会话索引由 **loginserver 在 ValidateLoginToken gRPC 校验通过时写入**
   （sgate 过门校验携带 `sgate_id/conn_id/client_ip`；HTTP `/api/v1/login` 是预铸 loginToken
   的工具接口，不写会话索引）
3. 踢人是**单向 RPC**：loginserver → sgate（Kick），sgate 本地查表执行，不落任何数据
4. 踢人失败**不回滚封禁**：权威库先写、踢 best-effort，失败仅告警+计数
5. sgate 无 admin 封禁端点，运维入口唯一

## 2. 会话索引（Redis，仅 loginserver 读写）

**写入点**：sgate LoginGate 过门校验调用 gRPC `ValidateLoginToken` 时携带
`sgate_id/conn_id/client_ip`；loginserver 校验通过后同步写四键（best-effort，写失败
仅告警不阻断校验；BatchValidate 复用同消息但不带上下文，天然跳过）。
索引只是踢人路由，不是在线凭证：

| 键 | 类型 | 值/成员 | TTL |
|---|---|---|---|
| `sess:conn:{sgateId}:{connId}` | hash | `{account_id, ip, login_at}` | 24h |
| `sess:acct:{accountId}` | set | 成员 `{sgateId}:{connId}` | 24h |
| `sess:ip:{ip}` | set | 成员 `{sgateId}:{connId}` | 24h |
| `sess:ip24:{a.b.c.0/24}` | set | 成员 `{sgateId}:{connId}` | 24h |

- `sess:ip24` 仅 IPv4 写入（CIDR 踢人路由用）；IPv6 无该键
- 无主动清理：连接关闭不删除索引，靠 TTL 24h 自然过期收敛（踢人路由可能含少量死条目，
  无效 Kick 对已关闭连接无副作用）；超长在线（>24h）踢不中由 BatchValidate 周期安全网兜底

## 3. 封禁链路

### 3.1 封禁（HTTP Admin，先库后踢）

```
POST /api/v1/acl/blacklist | /api/v1/acl/banned-role
1. 写 MySQL（权威，upsert）
2. 应用本地快照（本节点立即生效）
3. best-effort 踢人（每台 sgate 2s 超时，失败不回滚）：
   a. 路由：
      纯 IP  → SMEMBERS sess:ip:{exact} 解析 sgateId 去重
      CIDR   → sess:ip24 前缀键并集展开（/24 及更小精确路由；
               /23 及更大或 IPv6 → 向全部已知 sgate 下发，各台本地前缀匹配）
      账号   → SMEMBERS sess:acct 解析 sgateId 去重
   b. 逐台 KickIP(ip / CIDR) / KickAccounts([accountId...])，
      sgate 本地查表踢连接并回计数
4. 响应带 kicked/missed 统计
```

**落库顺序必须是 1→2→3**：封禁是权威操作，不能依赖踢人成功（sgate 不可达/进程崩溃
时封禁仍生效；被踢漏的人重登会被登录校验拦截）。

### 3.2 解封（HTTP）

删 MySQL 行 + 应用本地快照；**不**做任何会话动作（被踢的人重登自然恢复）。

### 3.3 登录校验

- **Login HTTP**（绑定账号后按 accountId 查快照）——权威拦截，命中不发 token
- **gRPC ValidateLoginToken**（sgate 过门时调，读同一快照）——封禁后已登录会话
  的重校验立即失效

### 3.4 周期安全网（BatchValidateLoginTokens）

Login HTTP 成功 → 玩家过 sgate 登录门之间存在极小封禁窗口，且会话索引 TTL 过期后
踢不中。sgate 登录成功后留存 loginKey，每 `batchInterval`（默认 60s）分批
（`batchSize`，默认 200）重校验全部在线会话，失效/被封禁的本地踢下线，收口上述窗口。

## 4. ACL 读写模型（进程内全量快照）

- 启动时从 MySQL 全量加载白/黑名单/封禁角色进内存快照（失败即启动失败）
- 读路径查快照：纯 IP 进 exact 集合（O(1)），CIDR 进前缀切片（条目量小，线性扫）
- 写路径落库后即时应用增量（写锁串行化，并发不丢更新）
- 多节点一致性依赖 MySQL 权威源 + 节点重启收敛（写少读多，可接受短暂不同步）

## 5. 踢人下发（loginserver → sgate）

- **SgateClientPool**：`ugrpc.Pool[protoKick.KickClient]`，源为 etcd `SGATE:{zone}`
  Discovery（sgate 自注册 JSON 地址，经地址变换解析出 gRPC 拨号地址）
- 鉴权：单一 `x-internal-token`（loginserver 拨号携带 / sgate 校验，配置同值）
- sgate 侧踢人动作统一为 `kickConn`：先发 KickNotify 再关闭（SendThenClose 保证
  客户端收到通知后才断连），登出/维护/Kick RPC 共用

## 6. 反代真实客户端 IP

- **WS（HTTP 握手）**：`trustedProxies`（IP/CIDR 列表）命中时从 `X-Real-IP`
  （优先）/`X-Forwarded-For`（最左项）解析真实 IP
- **TCP（无 HTTP 头）**：sgate 侧 `proxyProtocol` 开关，首包解析 PROXY protocol v1
- 均默认关闭；开启前需确认反代拓扑

## 7. 已知取舍

| 决策 | 说明 |
|---|---|
| 无 TTL 临时封禁 | 仅永久封禁入权威库 |
| 无 Bind/Unbind 双向 RPC | 会话索引写入点在 ValidateLoginToken，反向绑定冗余 |
| 会话索引无主动清理 | TTL 自然过期 + BatchValidate 兜底，避免 sgate 反向依赖 |
| 无全局 Session 中心服务 | Redis 四键已满足路由需求，独立服务运维面收益不成比例 |
| Kick 无可靠投递层 | 最终一致场景（登录校验 + BatchValidate 兜底），可靠性层复杂度不成比例 |
| ACL 无跨节点广播 | 写少读多，MySQL 权威 + 重启收敛足够 |

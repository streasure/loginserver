# loginserver

登录服务器：HTTP 提供登录/版本/管理接口（端口 10001），gRPC 提供内部登录校验服务（端口 10002），
pprof（10003）、负载采样 API（10004）。

## 配置

| 文件 | 用途 |
|------|------|
| `config/loginserver.yaml` | 常规配置：etcd / MySQL / Redis / uload 自适应准入 / pprof 等 |
| `config/tlog.yaml` | 常规日志配置（info 级别，写 `./logs/loginserver.log`） |
| `config/loginserver_stress.yaml` | 压测配置：uload 自适应准入全部关闭（不做 CPU/内存限流），仅用于吞吐测量 |
| `config/tlog_stress.yaml` | 压测日志配置：仅 error 级别，避免日志 IO 影响吞吐 |

启动：

```bash
scripts\start.bat                                                    # 构建并以常规配置启动
scripts\start.bat config\loginserver_stress.yaml config\tlog_stress.yaml   # 压测配置
```

或直接：

```bash
loginserver.exe -conf config\loginserver.yaml -logger config\tlog.yaml
```

配置中的 `redis` 段由 `uredis.Config` 解析（`instances` 组织实例，池/时长参数未配置即用默认值），
详见 util 文档 uredis 章节。

`db` 段由 `ugorm.Config` 解析（`configs` 组织实例，`maxIdleConns`/`maxOpenConns`/
`maxIdleSeconds`/`maxLifeSeconds` 为全局池参数），形态与业务参考配置一致，
详见 util 文档 ugorm 章节。`acl` 段为黑白名单/封禁角色与管理接口配置
（`cacheTtl` 服务器列表 Redis 缓存 TTL、`adminToken` 管理接口鉴权、
`trustedProxies` 信任的反代 IP/CIDR 列表——默认空 = 只信 TCP 对端，防
`X-Forwarded-For` 伪造；部署在反代/SLB 之后时必须配置，否则对端 IP 取到的是反代地址）。
`adminToken` 可用环境变量 `LOGINSERVER_ACL_ADMIN_TOKEN` 覆盖（生产避免明文落盘，优先级高于 yaml）。

`sgate.internalToken` 是封禁踢人链路（Kick RPC）的内部鉴权凭据，与 sgate 侧
`kickInternalToken` 同值，缺失时启动即失败。

## 数据库

建库建表脚本（MySQL 8，utf8mb4）：

| 文件 | 用途 |
|------|------|
| `sql/create_database.sql` | 创建 `loginserver` 库 |
| `sql/create_tables.sql` | 建表：`whitelist_ip`（IP白名单）、`blacklist_ip`（IP黑名单）、`banned_role`（封禁角色）、`server_info`（服务器信息列表） |

```bash
mysql -h 127.0.0.1 -u root -proot < sql/create_database.sql
mysql -h 127.0.0.1 -u root -proot < sql/create_tables.sql
```

## 黑白名单 / 封禁角色（ACL）

登录 `/api/v1/login` 每次执行两段检测：

1. **IP 黑白名单**（绑定账号前，按对端 IP）：白名单放行并跳过黑名单检查，命中黑名单返回
   `code=1004`（ip blocked）；
2. **封禁角色**（绑定账号后，按 accountId）：命中返回 `code=1005`（role banned），
   不再发放 loginToken。

token 校验（HTTP `/api/v1/validate/token` 与 sgate gRPC `ValidateLoginToken`）在 token
匹配通过后同样检测封禁角色：封禁后已登录会话的重校验立即失效（不主动踢在线，重连即拦），
token 不匹配直接返回 `valid=false`。

读写模型（进程内全量快照，无 Redis 依赖）：

- 启动时从 MySQL 全量加载白/黑名单/封禁角色进内存快照，加载失败即启动失败；
- 读路径查快照：纯 IP 进 exact 集合（O(1)），CIDR 进前缀切片（条目量小，线性扫）；
- 写路径落库后即时应用增量到本地快照（写锁串行化，并发不丢更新）；
- 多节点一致性依赖 MySQL 权威源 + 节点重启重新加载（写少读多，可接受短暂不同步）。

### 管理接口（HTTP）

请求头 `X-Admin-Token` 必须等于配置 `acl.adminToken`（服务器列表除外），失败返回 401 + `code=1006`。
增删直接写 MySQL 并同步应用本地快照。

| 方法 | 路径 | 请求体 | 说明 |
|------|------|--------|------|
| POST | `/api/v1/acl/whitelist` | `{"ip":"1.2.3.4","remark":"...","operator":"gm01"}` | 新增白名单 IP（operator 落库审计，可选；支持 CIDR） |
| DELETE | `/api/v1/acl/whitelist` | `{"ip":"1.2.3.4","operator":"gm01"}` | 删除白名单 IP（operator 仅审计日志） |
| GET | `/api/v1/acl/whitelist` | `?limit=` | 白名单列表 |
| POST | `/api/v1/acl/blacklist` | `{"ip":"1.2.3.4","remark":"...","operator":"gm01"}` | 新增黑名单 IP（落库后 best-effort 踢该 IP 在线连接） |
| DELETE | `/api/v1/acl/blacklist` | `{"ip":"1.2.3.4","operator":"gm01"}` | 删除黑名单 IP |
| GET | `/api/v1/acl/blacklist` | `?limit=` | 黑名单列表 |
| POST | `/api/v1/acl/banned-role` | `{"roleId":"...","reason":"...","operator":"gm01"}` | 封禁角色（落库后 best-effort 踢该账号在线连接） |
| DELETE | `/api/v1/acl/banned-role` | `{"roleId":"...","operator":"gm01"}` | 解除角色封禁 |
| GET | `/api/v1/acl/banned-role` | `?limit=` | 封禁角色列表 |
| GET | `/api/v1/sessions` | `?limit=&sgateId=` | 在线会话列表（读 Redis 会话索引） |
| GET | `/api/v1/sessions/account` | `?accountId=` | 账号 → 在线 sgate 反查 |
| GET | `/api/v1/servers` | - | 服务器列表（读缓存，无需 token） |

业务错误码：`1004` IP 命中黑名单、`1005` 角色被封禁、`1006` 管理接口鉴权失败、`1007` 检测/增删失败。

### gRPC 接口（供 sgate 调用）

协议见 `protocol/loginserver/loginserver.proto`：

| RPC | 说明 |
|-----|------|
| `ValidateLoginToken` | 单次校验 loginToken（sgate LoginGate 过门时调）。携带会话上下文（`sgate_id/conn_id/client_ip`）时写入 Redis 踢人路由索引（TTL 24h），并记录 `last_login_server:{accountId}`（30 天 TTL，供登录 UI 推荐最近登录服务器）；封禁角色在此拦截 |
| `BatchValidateLoginTokens` | 批量校验（sgate 周期安全网，`loginValidation.batchInterval`）。一次 MGET 预取合并回源，单条失败按 invalid 处理 |

### 封禁踢人（单向 Kick 模型）

封禁操作与数据 100% 收敛本服务，sgate 退化为纯执行器：

1. HTTP Admin 写 MySQL（权威）→ 应用本地快照；
2. 按会话索引路由出持有目标连接的 sgate，逐台下发 Kick（每台 2s 超时，best-effort，
   失败仅告警计数、不回滚封禁）；纯 IP 走 `sess:ip` 精确索引，CIDR 走 `sess:ip24`
   前缀索引（/23 及更大 fallback 全量广播），账号走 `sess:acct`；
3. sgate 经 etcd `SGATE:{zone}` 自注册，本服务经 `SgateClientPool` 按实例路由，
   连接携带 `x-internal-token` 鉴权。

sgate 不反向调用本服务、不存封禁数据；会话索引由 `ValidateLoginToken` 写入，
连接关闭不主动清理，靠 TTL 24h 自然过期。整体设计见 `docs/architecture.md`。

## 压测

`tools/httpstress` —— HTTP 压测客户端，按轮询分发覆盖全部接口并输出分接口统计：

内置接口注册表（路径与请求体与服务端 dto 对齐）：

| 接口 | 路径 | 请求体 |
|------|------|--------|
| `login` | `/api/v1/login` | `{"openId":"stress-openid","ptid":1}` |
| `version` | `/api/v1/version` | `{}` |
| `validate` | `/api/v1/validate/token` | 真实账号令牌（压测前自动预登录 `/api/v1/login` 获取，命中 token 缓存路径；预登录失败则回退为未命中路径占位凭证） |

| 参数 | 说明 |
|------|------|
| `-base` | 服务基地址，默认 `http://127.0.0.1:10001` |
| `-endpoints` | 压测接口：`all`（默认，三接口轮询混合）或逗号列表 `login,version,validate` 的任意子集 |
| `-c` | 并发连接数（默认 100） |
| `-d` | 压测时长（默认 10s） |
| `-warmup` | 预热时长（默认 2s，预热不计入统计） |

输出整体与每个接口的 ok（2xx）/bad（非 2xx，如准入 503）/failed（传输失败）与
rps/avg/p50/p95/p99/p99.9/max 延迟；压测时长 ≥ 30s 时每 30s 输出一行 progress
（累计量与区间吞吐），用于长时压测观察趋势。

`tools/grpcstress` —— gRPC 压测客户端（ValidateLoginToken）：

| 参数 | 说明 |
|------|------|
| `-target` | gRPC 地址，默认 `127.0.0.1:10002` |
| `-c` | 并发 worker 数（默认 100） |
| `-conn` | 共享连接数（0 = 每 worker 一条连接） |
| `-account-id` / `-login-token` | 请求所用账号/令牌 |
| `-d` / `-warmup` | 压测时长 / 预热时长 |

示例：

```bash
go run ./tools/httpstress -endpoints all -c 100 -d 10s        # 三接口混合
go run ./tools/httpstress -endpoints login -c 100 -d 10s      # 单接口独占
go run ./tools/grpcstress -target 127.0.0.1:10002 -c 100 -d 10s
```

压测前用压测配置启动服务端（关闭准入限流、日志仅 error），得到的吞吐即服务端峰值能力：

```bash
loginserver.exe -conf config\loginserver_stress.yaml -logger config\tlog_stress.yaml
```

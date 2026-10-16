# loginserver

登录服务器：HTTP 提供登录/版本接口（端口 10001），gRPC 提供内部登录校验服务（端口 10002），
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
详见 util 文档 ugorm 章节。`acl` 段为黑白名单/封禁角色的缓存与管理接口配置
（`cacheTtl` 条目 TTL、`genRefresh` 版本快照刷新周期、`adminToken` 管理接口鉴权、
`trustedProxies` 信任的反代 IP/CIDR 列表——默认空 = 只信 TCP 对端，防
`X-Forwarded-For` 伪造；部署在反代/SLB 之后时必须配置，否则对端 IP 取到的是反代地址）。
`adminToken` 可用环境变量 `LOGINSERVER_ACL_ADMIN_TOKEN` 覆盖（生产避免明文落盘，优先级高于 yaml）。

## 数据库

建库建表脚本（MySQL 8，utf8mb4）：

| 文件 | 用途 |
|------|------|
| `sql/create_database.sql` | 创建 `loginserver` 库 |
| `sql/create_tables.sql` | 建表：`whitelist_ip`（IP白名单）、`blacklist_ip`（IP黑名单）、`banned_role`（封禁角色）、`server_info`（服务器信息列表） |
| `sql/update_operator.sql` | 已建表环境增量更新：白/黑名单增加 `operator` 审计字段（新环境无需执行） |

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

检测结果读 Redis 缓存（负缓存同样命中，稳定态每次登录 2~3 次 GET），miss 经 singleflight
合并回源 MySQL；缓存写入携带全局版本号（acl:gen，写库后 INCR），读侧本地版本快照每
`acl.genRefresh` 刷新一次——新增/删除在本节点立即生效，其他节点脏数据窗口 ≈ genRefresh，
`acl.cacheTtl` 兜底 redis 数据丢失/写失败场景；redis 整体不可用时绕过缓存直查 MySQL。

### 管理接口（HTTP）

请求头 `X-Admin-Token` 必须等于配置 `acl.adminToken`（服务器列表除外），失败返回 401 + `code=1006`。
增删直接写 MySQL 并同步刷新 Redis 缓存。

| 方法 | 路径 | 请求体 | 说明 |
|------|------|--------|------|
| POST | `/api/v1/acl/whitelist` | `{"ip":"1.2.3.4","remark":"...","operator":"gm01"}` | 新增白名单 IP（operator 落库审计，可选） |
| DELETE | `/api/v1/acl/whitelist` | `{"ip":"1.2.3.4","operator":"gm01"}` | 删除白名单 IP（operator 仅审计日志） |
| POST | `/api/v1/acl/blacklist` | `{"ip":"1.2.3.4","remark":"...","operator":"gm01"}` | 新增黑名单 IP |
| DELETE | `/api/v1/acl/blacklist` | `{"ip":"1.2.3.4","operator":"gm01"}` | 删除黑名单 IP |
| GET | `/api/v1/servers` | - | 服务器列表（读缓存，无需 token） |

业务错误码：`1004` IP 命中黑名单、`1005` 角色被封禁、`1006` 管理接口鉴权失败、`1007` 检测/增删失败。

### gRPC 接口（供 sgate 调用）

协议见 `protocol/loginserver/loginserver.proto`，同样落库并同步缓存：

| RPC | 请求 | 说明 |
|-----|------|------|
| `AddBlacklistIP` | `ip` / `remark` / `operator` | 增加黑名单 IP |
| `AddBannedRole` | `roleId` / `reason` / `operator` | 封禁角色（accountId），登录与 token 校验时拦截 |
| `RemoveBlacklistIP` | `ip` / `operator` | 移除黑名单 IP |
| `RemoveBannedRole` | `roleId` / `operator` | 解除角色封禁 |

`tools/grpc_admin` 模拟 sgate 调用：

```bash
go run ./tools/grpc_admin -mode blacklist -ip 1.2.3.4 -remark drill
go run ./tools/grpc_admin -mode remove-blacklist -ip 1.2.3.4
go run ./tools/grpc_admin -mode banrole -role <accountId> -reason drill
go run ./tools/grpc_admin -mode remove-banrole -role <accountId>
```

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

### ValidateLoginToken 基线（2026-10-16，封禁收敛 P1 后）

| 并发 | 连接 | 时长 | RPS | avg | p50 | p95 | p99 | p99.9 | max | failed |
|------|------|------|-----|-----|-----|-----|-----|-------|-----|--------|
| 100 | 10 | 30s | 61,284 | 1.63ms | 1.20ms | 2.99ms | 5.30ms | 11.97ms | 178.04ms | 0 |
| 200 | 20 | 30s | **67,612** | 2.96ms | 2.82ms | 5.65ms | 9.48ms | 17.58ms | 51.33ms | 0 |
| 500 | 50 | 15s | 65,492 | 7.63ms | 6.53ms | 16.30ms | 30.02ms | 50.82ms | 98.37ms | 0 |

峰值约 6.7 万 RPS（c=200）；压测请求不携带 sgate 上下文（无 sgate_id/conn_id/client_ip），
不触发会话三键写入，路径为 ACL 快照检测 + token 校验缓存（ttl=1s）+ Redis 回源。

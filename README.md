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
loginserver.exe -conf config\loginserver.yaml -logger config\tlog.yaml
```

配置中的 `redis` 段由 `uredis.Config` 解析（`instances` 组织实例，池/时长参数未配置即用默认值），
详见 util 文档 uredis 章节。

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

输出整体与每个接口的 completed/failed/rps/avg/p50/p95/p99/p99.9/max 延迟。

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

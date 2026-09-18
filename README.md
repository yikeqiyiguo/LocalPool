# LocalPool —— 局域网闲置算力调度中心

LocalPool 是一个**轻量、开箱即用**的算力调度平台：把办公室/机房/家里一批台式机（含个人工作电脑）的闲置算力汇聚成一个任务池，统一提交、调度、执行与观测。

- **调度中心（Server）**：单二进制，内置 Web 控制台与 REST/SSE 接口，SQLite 持久化（零 CGO、零外部依赖）。
- **执行节点（Agent）**：部署到任意 Windows / Linux 机器上即可贡献算力，支持**人机共存**——机器被同事使用时自动避让/暂停/终止任务。
- **控制方式**：浏览器访问 Server 即用，也可直接调用 HTTP API 集成到脚本/CI。

```
┌─────────────────────────────── 调度中心 localpool-server (默认 :8080) ───────────────────────────────┐
│  Web 控制台(内嵌 Vue)   REST API(/api/*)         调度引擎(1s 轮询)         SQLite data/localpool.db │
└───────────────────────────────▲──────────────────────────▲──────────────────────────────────────────┘
                      心跳/注册/日志/结果 │ HTTP/JSON + SSE  │ 心跳应答下发任务
                       ┌─────────────────┴───────────────┐
                   Agent A (Windows 办公机)         Agent B (Linux 渲染机)   …… N 台
```

调度采用 **Agent 主动拉取**模型：Agent 周期性上报心跳，调度中心在心跳应答里携带"下发任务/取消指令"。无反向长连接，天然适合跨机器与常见 NAT 局域网。

## 功能特性

| 能力 | 说明 |
| --- | --- |
| 任务管理 | 创建、取消、手动重试、删除、筛选/搜索、实时日志（SSE 直播）、历史日志分页 |
| 智能调度 | 按优先级（低/普通/高）与先到先得出队；"最闲优先"选节点；自动规避队头阻塞 |
| 资源限制 | 单任务超时、内存上限（超限即终止）、CPU 上限（超限自动降权不终止） |
| 人机共存 | 仅把任务发给真正空闲的机器；有人使用时可自动终止 `need_idle` 任务并重试 |
| 高可用对账 | 排队→运行 CAS 抢占；心跳对账消灭"僵尸/重复执行"；节点离线自动判失并自愈 |
| 自动重试 | 节点丢失/被占用等异常可按次数自动重试（可关闭） |
| 内置控制台 | 单文件内嵌 Vue 前端，开箱即用；也可分离 Vite 开发 |
| 访问鉴权 | 可选 `-secret` 密钥，Web / API / Agent 统一校验（恒定时间比较） |
| 零 CGO | 纯 Go + modernc SQLite，交叉编译友好；Agent 仅支持 Windows / Linux |

## 目录结构

```
cmd/localpool-server    调度中心入口
cmd/localpool-agent     节点 Agent 入口
internal/protocol       Server↔Agent 协议与共享常量/枚举
internal/server         调度引擎、SQLite 存储、REST/SSE/Agent 接口、设置
internal/agent          节点端：注册、心跳、进程树采样、日志回传、idle 检测
internal/webui          go:embed 内嵌前端构建产物 (dist)
web/                    Vue 3 + Element Plus 前端源码
```

## 快速开始

### 1. 环境要求

- Go ≥ 1.26（编译与运行均无需 CGO）
- 运行平台：Windows / Linux（Agent 不支持 macOS）
- 无需安装任何数据库 / 运行时依赖

### 2. 构建

在项目根目录执行：

```bash
# Windows PowerShell
go build -o bin/localpool-server.exe ./cmd/localpool-server
go build -o bin/localpool-agent.exe  ./cmd/localpool-agent

# Linux / macOS
go build -o bin/localpool-server ./cmd/localpool-server
go build -o bin/localpool-agent  ./cmd/localpool-agent
```

生成的 Server 已内嵌 Web 前端，**只需这一个文件**即可运行整套平台。

交叉编译（例如在 Windows 上为 Linux 机器编译 Agent）：

```bash
$env:GOOS='linux'; $env:GOARCH='amd64'
go build -o bin/localpool-agent-linux-amd64 ./cmd/localpool-agent
Remove-Item Env:GOOS; Remove-Item Env:GOARCH
```

### 3. 启动调度中心

```bash
localpool-server -addr :8080 -data ./data
```

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-addr` | `:8080` | HTTP 监听地址；局域网接入需监听所有网卡（默认已如此），注意防火墙放行 |
| `-data` | `./data` | 数据目录（SQLite：`data/localpool.db`，任务/节点/日志/设置全在这里） |
| `-secret` | 空 | 访问密钥。非空时所有 `/api`、`/agent` 请求必须携带相同密钥；Web 首次访问会提示输入 |
| `-dev` | `false` | 仅启动 API（不内嵌前端），配合 Vite 开发服务器使用 |

启动后浏览器打开 `http://<服务器IP>:8080/` 即可看到控制台。

### 4. 接入执行节点（Agent）

在每台愿贡献算力的机器上运行：

```bash
# 本机演示
localpool-agent -server http://127.0.0.1:8080

# 局域网节点（换成 Server 机器 IP）
localpool-agent -server http://192.168.1.10:8080 -name "设计组-王工" -secret 你的密钥
```

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-server` | `http://127.0.0.1:8080` | 调度中心地址 |
| `-name` | 主机名 | 节点显示名，便于在控制台识别 |
| `-data` | `./agent-data` | 本地数据目录（`agent.json`） |
| `-secret` | 空 | 须与调度中心一致；传入后会自动持久化到配置 |

节点 ID（`node_id`）在首次注册后持久化到 `agent-data/agent.json`，**重启、换目录重新部署都不会改变身份**，历史归属记录保持连续。

> 如需更换调度中心地址：直接编辑 `agent-data/agent.json` 中的 `server` 字段后重启，或换用新的 `-data` 目录。

### 5. Web 控制台

| 页面 | 路由 | 用途 |
| --- | --- | --- |
| 算力总览 | `/dashboard` | 在线节点/核数/内存/平均负载、任务状态计数、24h 成败、被占用中的人机共存节点 |
| 节点管理 | `/nodes` | 每台机器实时 CPU/内存/磁盘/网络、在线状态、人类活动状态、运行中任务数 |
| 任务中心 | `/tasks` | 创建任务、列表筛选搜索、打开实时日志抽屉、取消/重试/删除 |
| 历史统计 | `/stats` | 最近 24h 起（可拉长）按时段聚合的成功/失败/运行分布 |
| 调度设置 | `/settings` | 全局调度与人机共存策略（实时生效） |

## 使用指南

### 创建任务

以 Shell 命令为单位提交（多行命令可直接粘贴）。字段说明：

| 字段 | 说明 |
| --- | --- |
| 任务名称 | 留空自动取命令前 40 字符 |
| Shell 命令 | 必填。交给执行机器默认 Shell 执行 |
| 工作目录 | 可选，进程启动目录 |
| 环境变量 | 可选，每行一条 `KEY=VALUE` |
| 优先级 | 低 / 普通 / 高（决定队列出队顺序） |
| 超时时间 | 超过即被杀并标记超时（默认取全局默认值 3600s） |
| Shell | 自动 / cmd / PowerShell / bash（按执行机器平台匹配，见下） |
| 仅空闲机器执行 | **默认开启**（见"人机共存"一节） |
| 内存上限 | 超限即终止（进程树 RSS，0=不限） |
| CPU 上限 | 超限时降低进程优先级并持续提示，不终止（0=不限） |

Shell 实际执行方式：

| 你在 Web 选 | Windows 节点 | Linux 节点 |
| --- | --- | --- |
| 自动 | `cmd /C` | `bash -lc`（缺 bash 时 `sh -c`） |
| cmd | `cmd /C` | 落到 bash |
| PowerShell | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass` | 落到 bash |
| bash | 落到 cmd | `bash -lc` |

> 说明：任务端到端会**整树**管理进程（不只杀外壳），可干净终止多级子进程。

### 实时日志

- 任务运行中打开任务详情即可 **SSE 实时直播** stdout/stderr（回传日志会标 `out` / `err`），断线自动用历史补齐、不丢行。
- 任务结束后推送 `done` 事件自动收敛，日志保留在服务端，可按 `tail` / `after` 分页回看。
- 任务超时、内存超限、取消、避让终止等系统事件也会以 `err` 行写入日志。

### 取消 / 重试 / 删除

- **取消**：排队中立即移出队列；运行中会向所在节点下发终止指令（整树杀进程）。若指令丢失，心跳对账会自动补发，保证进程最终收敛。
- **重试**：复制原任务（命令/环境/限制全部保留）重新排队，`retry_of`/`retry_count` 记录链路。终态任务均可手动重试。
- **删除**：仅允许删除终态任务（含其历史日志），运行中任务请先取消。

### 人机共存（核心功能）

LocalPool 特别适合把**个人工作电脑**纳入算力池：

1. **闲置判定**：Agent 每次心跳（默认 2s）报告"距离最近一次键鼠操作的空闲秒数"。
   - Windows：系统级 `GetLastInputInfo`，可靠；
   - Linux：使用 `xprintidle`（桌面环境）；**未安装则无法感知，该机器被视为始终空闲**。
2. **空闲阈值**：默认 300s（5 分钟）无键鼠操作即视为"空闲"。可在"调度设置"调整。
3. **任务标记**：创建任务时勾选"仅空闲机器执行"（默认勾选），调度器只会把它放到空闲机器。
4. **占用避让**：空闲机器一旦被用户使用：
   - 若设置 `避让时终止运行中任务`（默认开）→ 该机器上所有 `need_idle` 任务被终止（原因 `human_evict`），并按"自动重试次数"在其它空闲机器重新排队；
   - 否则 → 不再下发新任务，已运行的让它自然跑完。

行为矩阵（`human_idle_threshold` 阈值、`need_idle` 任务）：

| HumanEnabled | EvictOnHumanActive | need_idle 任务行为 |
| --- | --- | --- |
| 开 | 开 | 只发空闲机；机器被占用即终止并在空闲机重试 |
| 开 | 关 | 只发空闲机；运行中被占用不终止，但之后不再发给占用机 |
| 关 | — | 忽略"仅空闲"标记，按普通任务任意调度、不避让（总开关） |

> 关闭"人机共存"总开关的用途：例如机房/服务器完全无人使用，希望所有任务随时全速跑。

### 调度设置项

在"调度设置"页修改，保存后实时生效并持久化（存于 SQLite `kv` 表）。

| 字段（JSON key） | 默认 | 说明 |
| --- | --- | --- |
| `human_enabled` | `true` | 人机共存总开关 |
| `evict_on_human_active` | `true` | 空闲机被占用时是否终止其上的 need_idle 任务 |
| `node_idle_threshold_sec` | `300` | 距最近一次键鼠操作多久算"空闲"（最小 10） |
| `heartbeat_ms` | `2000` | 期望 Agent 心跳间隔（最小 500） |
| `offline_grace_ms` | `8000` | 心跳超时多久判定节点离线（最小 3000） |
| `max_tasks_per_node` | `2` | 单节点最大并发任务数 |
| `max_retries` | `0` | 节点丢失/占用等异常后的自动重试次数（0=不自动重试） |
| `dispatch_interval_ms` | `1000` | 调度扫描间隔（最小 200） |
| `default_timeout_sec` | `3600` | 创建任务未填超时时的默认值 |
| `web_title` | LocalPool … | 控制台页面标题 |

## 任务状态与结束原因

状态机：`queued`（排队）→ `running`（运行）→ `success` / `failed` / `canceled`（终态）。

运行中任务被终止的可能原因（任务详情可见，日志同步提示）：

| 原因 | 含义 |
| --- | --- |
| `timeout` | 超过任务超时时间 |
| `mem_limit` | 内存超过程序树上限 |
| `cpu_throttle` | CPU 超过软上限（不终止，仅降权，作为提示记录） |
| `user_cancel` | 用户取消 |
| `human_evict` | 人机避让：节点被用户占用 |
| `node_lost` | 节点离线/进程丢失（心跳超时或对账发现进程消失） |
| `agent_quit` | Agent 进程退出前终止了其运行的任务 |

## HTTP API

统一前缀 `/api`，响应体 `{ "code": 0, "msg": "ok", "data": ... }`。启用 `-secret` 后须带请求头 `X-API-Key: <密钥>`（Agent 使用 `X-Agent-Key`，值相同）。

```bash
BASE=http://127.0.0.1:8080/api
# 需要密钥时:
# AUTH='-H "X-API-Key: xxx"'
```

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/overview` | 全局大盘（节点/核数/内存/任务计数/被占用节点） |
| GET | `/nodes` | 节点列表 |
| GET | `/nodes/:id` | 单个节点 |
| GET | `/tasks?status=&node=&q=&page=&size=` | 任务列表（分页） |
| GET | `/tasks/summary` | 按状态计数 |
| POST | `/tasks` | 创建任务（见下方示例） |
| GET | `/tasks/:id` | 任务详情（含 `log_lines`） |
| DELETE | `/tasks/:id` | 删除终态任务 |
| POST | `/tasks/:id/cancel` | 取消（body 可选 `{reason}`） |
| POST | `/tasks/:id/retry` | 复制重试 |
| GET | `/tasks/:id/logs?tail=200` | 取最近 N 行；或 `?after=<id>&limit=1000` 增量取行 |
| GET | `/tasks/:id/logs/stream?after=0` | SSE 实时日志流 |
| GET | `/stats/timeline?hours=24` | 任务时间分布（1–720 小时） |
| GET/PUT | `/config` | 读写全局设置 |
| GET | `/agent/ping` | 匿名探活（返回 `pong`），供监控使用 |

创建任务示例：

```bash
curl -s "$BASE/tasks" -X POST $AUTH -H 'Content-Type: application/json' -d '{
  "name": "视频转码",
  "command": "ffmpeg -i in.mp4 -c:v libx264 -crf 23 out.mp4",
  "cwd": "D:/works",
  "env": { "CUDA_VISIBLE_DEVICES": "0" },
  "priority": 1,
  "timeout_sec": 7200,
  "max_mem_mb": 4096,
  "max_cpu_pct": 0,
  "need_idle": true,
  "shell": "auto"
}'
```

SSE 日志流（示例：跟随任务 ID 实时输出日志，收到 `done` 即结束）：

```bash
curl -N "$BASE/tasks/<任务ID>/logs/stream?after=0" $AUTH
```

## 可靠性设计（供集成方了解语义）

- **CAS 抢占**：排队→运行使用条件更新，取消/删除竞态下不会把"已取消任务"派出去执行。
- **终态保护**：Agent 迟到的执行结果不会覆盖已收敛的终态（离线误判、超时迁移、用户取消等场景），只补记实测统计。
- **心跳对账**：服务端重启丢失下发记录时，Agent 上报的 running 任务会被认领；DB 已终态而进程仍活时自动补发终止，杜绝"僵尸进程/双跑"。
- **节点离线自愈**：心跳超时（默认 8s）→ 任务判 `node_lost` → 按 `max_retries` 在其它节点自动重试。
- **日志可靠**：Agent 按批落库并回推，SSE 连接补历史补订阅窗口，行号(`id`)单调，客户端可增量续传去重。

## 前端二次开发

控制台为 Vue 3 + Element Plus。修改前端后需重新构建，产物被 `go:embed` 进 Server 二进制：

```bash
cd web
npm install

# 方式一：开发热更新（同时开两个终端）
npm run dev                       # http://localhost:5173，/api 代理到 127.0.0.1:8080
go run ./cmd/localpool-server -dev

# 方式二：生产构建（输出到 internal/webui/dist 并重新编译 Server）
npm run build
cd ..
go build -o bin/localpool-server.exe ./cmd/localpool-server
```

## 开机自启 / 部署建议

Server 与 Agent 均为控制台程序，生产建议托管：

**Windows（计划任务）**

```powershell
schtasks /Create /TN "LocalPool Server" /TR "D:\localpool\localpool-server.exe -addr :8080 -data D:\localpool\data -secret xxx" /SC ONSTART /RU SYSTEM /RL HIGHEST
```

**Linux（systemd，Agent 示例）**

```ini
# /etc/systemd/system/localpool-agent.service
[Unit]
Description=LocalPool Agent
After=network-online.target

[Service]
ExecStart=/opt/localpool/localpool-agent -server http://192.168.1.10:8080 -name node1 -secret xxx
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload && sudo systemctl enable --now localpool-agent
```

## 注意事项

- Agent 仅支持 **Windows / Linux**；macOS 请放到 CI/容器等其它执行器（不在当前支持范围）。
- **Linux 无桌面（服务器）无法感知"人是否在"，会被视为始终空闲**，适合机房/服务器场景；若想让这类机器不受人机策略影响，可直接关闭人机共存总开关。
- HTTP 为明文 + 可选静态密钥。请务必启用 `-secret` 并仅在可信局域网使用；如需公网请自行置于反向代理（HTTPS）之后。
- 任务为"尽力执行"模型：进程被系统杀、机器断网断电等极端情况由调度器判 `node_lost` 并按设置自动重试，任务需具备**可重复执行**的幂等性。
- 停止调度中心不影响已在线 Agent（任务继续在本机跑），恢复后心跳自动续上；Agent 进程被强制杀掉时，其运行任务需等心跳超时判离线后由调度中心处理。
- 节点贡献算力默认最大并发 2 个任务、优先级排队，可通过 `max_tasks_per_node`、任务优先级调整。

## FAQ

**Q：任务一直显示排队，为什么？**
依次排查：① 是否没有在线节点；② 是否勾了"仅空闲机器执行"且所有在线节点均处于被占用状态（`human_active`）；③ 是否单节点并发已满；④ 节点 CPU/内存上限是否满足任务申请。

**Q：Agent 启动提示无法连接调度中心？**
确认 Server 已启动、`-server` 地址正确、防火墙放行 8080、`-secret` 一致。可用 `curl http://<server-ip>:8080/agent/ping` 测试连通。

**Q：节点掉了/重装了，为什么任务显示 node_lost 失败？**
心跳超过 `offline_grace_ms`（默认 8s）即判离线。若希望自动换节点重跑，在设置里调大 `max_retries`。

**Q：如何迁移/备份数据？**
拷贝 Server 的 `data/` 目录即可（SQLite，含任务、日志、节点、设置）。Agent 端为 `agent-data/agent.json`（身份与连接信息）。请先停服务再拷贝以保证一致性。

**Q：想彻底清空重来？**
停掉 Server 删除 `data/` 目录；Agent 换 `-data` 目录或删 `agent.json`（会以新身份重新注册）。

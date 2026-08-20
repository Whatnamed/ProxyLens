# Mihomo Discovery Probe (tools/discovery)

> ProxyLens Phase 0 — 专用于验证 Mihomo External Controller 真实数据语义的最小只读调研探针。

---

## 1. 工具定位与用途

本工具是一个**仅用于前期调研、一次性、严格只读**的轻量探针脚本。
它的唯一目的，是在真实 Windows + Mihomo 环境下，原样捕获 `/connections` 与 `/traffic` 的原始 WebSocket 数据流，供后续分析字段完备性、生命周期事件与流量增量模型。

**重要说明**：
- 零第三方 npm 依赖，依托 Node.js 22+ 原生标准库运行。
- 绝不修改任何 Mihomo 配置、代理规则、节点或 TUN 状态。
- 绝不在终端输出或提交任何 Controller Secret 与个人凭据。

---

## 2. 如何启动

### 2.1 基本命令
```bash
# 默认连接 http://127.0.0.1:9090
node tools/discovery/probe.mjs

# 指定 Controller 端口与持续时长 (例如采集 30 秒)
node tools/discovery/probe.mjs --controller http://127.0.0.1:9090 --duration 30

# 若 Controller 配置了 Secret，可通过参数或环境变量传入
node tools/discovery/probe.mjs -c http://127.0.0.1:9090 -s your_secret_here
# 或者
$env:MIHOMO_SECRET="your_secret_here"; node tools/discovery/probe.mjs
```

### 2.2 参数说明

| 参数 | 缩写 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- |
| `--controller` | `-c` | `http://127.0.0.1:9090` | Mihomo External Controller 地址 |
| `--secret` | `-s` | 无 (或读取 `MIHOMO_SECRET` 环境变量) | Controller 鉴权密钥 (如有) |
| `--output` | `-o` | `tmp/discovery/<timestamp>/` | 原始样本保存目录 |
| `--duration` | `-d` | `0` (持续运行) | 采集持续秒数，到期自动退出 |
| `--help` | `-h` | - | 查看帮助信息 |

---

## 3. 输出文件与目录结构

所有采集样本默认输出到已被 `.gitignore` 排除的 `tmp/discovery/<session-id>/` 目录中：

- `manifest.json`：记录探针版本、开始/结束时间、Mihomo 版本、脱敏后的配置与统计帧数。
- `connections.ndjson`：原样保存收到的每一帧 `/connections` 快照（每行一条 JSON，附带接收时间戳 `receivedAt`）。
- `traffic.ndjson`：原样保存收到的每一帧 `/traffic` 速率数据。
- `events.ndjson`：记录探针生命周期中的诊断事件（启动、连接成功、断线、异常、优雅关闭等）。

---

## 4. 如何停止

- **手动停止**：在终端随时按下 `Ctrl + C`，探针将捕获中断信号，安全关闭 WebSocket 连接并完整刷新写入磁盘，退出状态标记为 `interrupted_by_user`。
- **定时停止**：指定 `--duration <秒数>`，到达设定时长后自动平滑关闭。

---

## 5. 安全与 Git 纪律

1. **样本绝不入库**：抓取的原始连接可能包含真实的访问域名、内网 IP 与进程绝对路径，所有样本必须留在 `tmp/` 目录中，严禁提交到 Git。
2. **Secret 绝不泄露**：命令行参数与环境变量中的 Secret 仅在建立握手时作为 Token 传递，`manifest.json` 与控制台日志中一律进行脱敏 (`***REDACTED***`) 处理。

---

## 6. 下一步：如何配合触发测试场景 (Phase 0C 预告)

在后续的 Phase 0 场景矩阵测试中，用户可在运行本探针的同时，在系统中执行对应的网络行为以产生真实样本：
1. **后台 NTP/UDP**：观察后台时间同步或安全软件触发的 UDP 123 连接。
2. **浏览器访问**：打开特定网站（如 HTTP/1.1、HTTP/2、QUIC 网页）。
3. **代理大文件下载**：通过代理节点下载测试文件，观察累积流量变化。
4. **DIRECT 直连大流量**：通过直连网络下载内容，验证直连标记。
5. **节点切换与重启**：在客户端中切换代理节点或重启内核，观察连接链 (chains) 历史与重连表现。

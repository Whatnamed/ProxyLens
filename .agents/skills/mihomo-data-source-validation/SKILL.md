---
name: mihomo-data-source-validation
description: Validates Mihomo External Controller data for ProxyLens. Use for Phase 0 research involving /connections, /traffic, process and host coverage, rules, proxy chains, connection lifecycle, DIRECT/PROXY classification, traffic deltas, restarts, or MetaCubeXD comparison.
---

# Mihomo Data Source Validation

本 Skill 用于 ProxyLens 的 Phase 0 数据源验证，以及后续任何需要重新确认 Mihomo External Controller 数据语义的任务。

目标不是“尽快写出 Collector”，而是先用可复现证据回答：Mihomo 实际提供了什么、哪些字段稳定、哪些字段会缺失、连接和流量计数如何变化。

## 1. 开始前

先读取：

1. `AGENTS.md`
2. `docs/STATUS.md`
3. `docs/ARCHITECTURE.md`
4. `docs/ROADMAP.md`
5. 与当前任务直接相关时再读取 `docs/PRODUCT.md`

如果用户刚提供了当前 Mihomo 版本、Controller 地址、TUN 状态或实际配置，以当前信息优先。

不要假设历史端口、Secret、节点、网络环境或 Mihomo 版本仍然有效。

## 2. 证据等级

所有技术结论必须标记为以下三类之一：

- **Documented**：Mihomo 官方文档或当前官方源码明确支持。
- **Observed**：在用户当前真实环境中已经采到并可复现。
- **Inferred**：由现象或第三方实现推断，但尚未直接验证。

第三方 Dashboard（包括 MetaCubeXD、Zashboard 等）的类型定义和算法只能作为参考，不能自动升级为 Mihomo 的 Documented 事实。

如果 Documented 与 Observed 不一致，优先保留真实样本，并记录版本和环境差异，不要静默“纠正”数据。

## 3. 安全与只读边界

Phase 0 只允许观察。

禁止：

- 修改 Mihomo 配置；
- 改规则或节点；
- 切换 TUN / 系统代理来“修问题”，除非测试场景明确要求且用户已知情；
- 抓取 HTTP / TLS 正文；
- 打印或提交 Controller Secret、代理用户名密码、订阅 URL、节点凭据；
- 把未脱敏的网络历史提交 Git。

原始 JSON、临时日志和测试脚本输出默认放在 Git 已忽略的 `tmp/` 下。

如果需要把样本写进文档，只保留证明结论所需的最小片段，并脱敏公网 IP、路径、凭据和其他不必要的个人信息。

## 4. 推荐验证流程

### Step A — 确认运行环境

记录：

- Windows 版本；
- Mihomo 版本；
- GUI / 启动方式；
- TUN 是否开启；
- `find-process-mode` 等会影响进程识别的相关配置；
- External Controller 的实际可达地址；
- 当前 Mihomo mode（Rule / Global / Direct）；
- 测试期间所选策略 / 节点，只记录必要的匿名化名称。

不要把 Controller Secret 写入报告。

### Step B — 建立最小只读探针

优先使用最小、一次性的调研脚本连接当前 Controller。

Phase 0 不要因此初始化完整 Go / Rust / Tauri 工程。

探针至少应能够：

- 连接 `/connections`；
- 原样保存带时间戳的若干帧；
- 观察 `/traffic` 在需要时的实时数据；
- 记录 WebSocket 断开和恢复；
- 不修改任何 Mihomo 状态。

临时代码和原始输出可放 `tmp/`。如果某个脚本后来值得成为长期测试工具，再单独决定是否正式纳入仓库。

### Step C — 先建立基线

在 TUN 开启、尽量不主动打开额外应用的情况下观察一段短基线。

目标：

- 看清空闲状态仍会有哪些系统 / 后台进程连接；
- 验证 process、host、rule、chains 等最基本字段；
- 为后续主动场景提供对照。

不要因为出现后台连接就立即判断为异常。

### Step D — 场景矩阵

完整 Phase 0 至少覆盖以下场景；单次小任务可只执行与问题有关的子集。

1. **后台 UDP / NTP**
   - 重点看 `network`、端口、process、host、rule、rulePayload、chains。
2. **浏览器 HTTPS / TCP**
   - 重点看 host、sniffHost、destinationIP、process、流量累计。
3. **QUIC / UDP 443**
   - 在能够稳定触发时验证，不为了“凑结果”强行假设已经走 QUIC。
4. **明确走代理的大流量连接**
   - 用于验证连接级累计字节与整体流量关系。
5. **明确 DIRECT 的大流量连接**
   - 用于验证 DIRECT 的真实字段与 Proxy Traffic 分类边界。
6. **长连接**
   - 观察同一 connection id 的多帧累计变化。
7. **大量短连接**
   - 观察连接出现 / 消失和帧间遗漏风险。
8. **策略或节点切换**
   - 验证历史连接的 chains 是否反映连接发生当时的路径。
9. **Mihomo 重载 / 重启**
   - 观察 WebSocket、connection id、全局计数和旧连接如何变化。
10. **Controller 断开 / 恢复**
   - 验证“监控缺口”和“正常连接结束”能否区分。

## 5. 每条连接重点字段

至少检查：

```text
id
start
metadata.network
metadata.type
metadata.sourceIP
metadata.sourcePort
metadata.destinationIP
metadata.destinationPort
metadata.host
metadata.sniffHost
metadata.dnsMode
metadata.process
metadata.processPath
upload
download
chains
rule
rulePayload
```

对每个场景记录：

- 是否存在；
- 是否为空；
- 是否稳定；
- 是否随时间变化；
- 若缺失，有什么可靠 fallback；
- 是否会影响“谁 / 去哪 / 为什么 / 走哪里 / 多少”的可解释链。

不要把 `host` 为空直接记成“Unknown”。如果有 `sniffHost` 或 `destinationIP`，应分别标记实际可用信息。

## 6. 必须验证的流量语义

重点回答：

1. `upload` / `download` 是连接级累计值还是瞬时值？
2. 同一 connection id 在连续帧中的值是否单调增加？
3. 新连接首次被看到时累计值可能已经大于 0 吗？
4. 连接结束前最后一帧是否一定能看到最终字节数？
5. WebSocket 重连后，同一真实连接是否保持原 id？
6. Mihomo 重启后计数和 id 如何变化？
7. `/connections` 顶层总量与单连接增量之和能否可靠对账？
8. `/traffic` 适合做速率观察、总量校验还是两者都可？必须基于真实接口语义说明。

在这些问题没回答前，不要设计正式 double-counting 算法。

## 7. DIRECT / PROXY / REJECT 与 Chains

不要先写分类规则再找数据证明它。

分别抓取真实：

- DIRECT；
- 单层代理；
- 多层链式代理；
- MATCH 兜底；
- 宽泛 UDP 规则；
- REJECT / DROP（如当前环境存在）。

记录：

- `rule`；
- `rulePayload`；
- `chains` 的真实顺序；
- 最终节点如何表达；
- DIRECT 是否有 chains；
- 策略组和实际节点是否都能从数据中恢复。

只有这些行为实测后，才能在 Architecture / Collector 中定义正式分类算法。

## 8. 字段覆盖率与 Unknown 分类

报告必须有覆盖率或至少场景化计数，不接受“基本都能识别”这种结论。

建议统计：

```text
process present / total
processPath present / total
host present / total
sniffHost present / total
host OR sniffHost present / total
host OR sniffHost OR destinationIP present / total
rule present / total
rulePayload present / total
chains present / total
```

并把“不完整”拆成真实原因，例如：

- MissingProcess
- MissingProcessPath
- MissingHostButSniffAvailable
- IPOnly
- MissingRule
- MissingChain
- MonitoringGap
- SampleNotCaptured

**MonitoringGap 永远不允许并入 Unknown。**

## 9. 对 MetaCubeXD 等参考实现的使用方式

可以研究其：

- `/connections` 消费方式；
- connection diff；
- Data Usage 持久化字段；
- IndexedDB / 历史连接处理；
- 页面关闭后的采集限制。

但必须分别写清：

- 这是参考实现自己的设计选择；
- 还是 Mihomo API 本身的事实。

不要因为参考项目丢弃了 rule / chains 等字段，就误认为 Mihomo 没提供这些字段；反过来也一样。

## 10. Phase 0 报告结构

完整调研结束后更新或创建：

`docs/research/mihomo-data-source.md`

建议结构：

```markdown
# Mihomo Data Source Validation

## Test Environment
## Evidence Levels
## API Behaviour
## Connection Field Coverage
## TCP Findings
## UDP / NTP Findings
## QUIC Findings
## DIRECT / PROXY / REJECT Semantics
## Chains Semantics
## Traffic Counter Semantics
## Connection Lifecycle
## Restart / Reconnect Behaviour
## Monitoring Gaps
## Reference Implementation Comparison
## Unknown Attribution Table
## Confirmed Conclusions
## Remaining Unknowns
## Implications for Collector Design
## Recommended Next Step
```

报告中每个关键结论尽量标记 Documented / Observed / Inferred。

## 11. 完成任务后的仓库维护

如果得到了会改变后续设计的证据：

- 更新 `docs/STATUS.md`；
- 必要时修正 `docs/ARCHITECTURE.md` 中已经被实测推翻或确认的假设；
- 如果 Phase 0 Acceptance 已全部满足，再更新 `docs/ROADMAP.md` 状态。

不要因为一次小测试就提前宣布 Phase 0 完成。

完成汇报应包括：

- 实际执行了哪些场景；
- 得到了哪些 Observed 结论；
- 哪些问题仍然未验证；
- 是否产生监控盲区或样本质量问题；
- 对 Phase 1 设计有什么直接影响；
- 下一步最值得验证什么。

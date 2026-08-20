# ProxyLens

> 面向 Windows + Mihomo / Clash 的本地代理流量审计工具。

---

## 什么是 ProxyLens？

**ProxyLens** 是一个运行在本地的代理流量审计与连接追踪工具。

当你在 Windows 下开启 Mihomo / FLClash 的 TUN 模式或系统代理时，各类后台进程、系统服务及 UDP/QUIC 连接可能在不经意间经过代理节点，产生不必要的代理流量或潜在的安全分流问题。

现有的 Clash / Mihomo 客户端通常仅提供实时的连接查看界面，连接一旦结束便无法回溯。ProxyLens 致力于长期、可靠地保存这些连接元数据，让代理流量**可追溯、可解释**，帮助用户发现异常分流并辅助优化 Mihomo 规则配置。

---

## 为什么需要 ProxyLens？

ProxyLens 的核心定位是**流量审计与分流优化辅助**，而非普通的“流量计费器”。

它旨在完整回答以下审计链条：

$$\text{谁 (进程)} \longrightarrow \text{去了哪里 (域名/IP)} \longrightarrow \text{命中何种规则} \longrightarrow \text{经过何种策略组} \longrightarrow \text{实际走哪个节点} \longrightarrow \text{消耗多少上传/下载}$$

典型示例：
```text
HipsDaemon.exe
  → us.pool.ntp.org:123
  → UDP
  → NETWORK,udp
  → 漏网之鱼/节点选择
  → 某实际代理节点
  → 上传 120 B / 下载 120 B
```
通过该记录，用户能迅速定位后台 NTP 同步被错误送入代理的问题，并针对性地为 Mihomo 添加 `DIRECT` 规则。

---

## 当前项目状态

- **阶段**：`Documentation / Discovery`（文档规范与需求基线建立阶段）
- **代码状态**：尚未开始编写业务代码，未引入框架与第三方依赖。
- **当前核心任务**：详见 [STATUS.md](file:///E:/Projects/ProxyLens/ProxyLens/docs/STATUS.md)。

---

## 第一阶段目标环境

- **操作系统**：Windows 11
- **代理内核**：Mihomo（支持 External Controller 接口）
- **搭配 GUI**：FLClash（或其它标准 Mihomo 前端）

> **说明**：ProxyLens 在架构上保持与具体客户端解耦，优先在上述标准环境下验证数据完备性与准确性。

---

## 主要能力摘要

- **历史连接审计**：持久化已结束连接的元数据（进程、目标、协议、规则、策略链、最终节点、流量、起止时间等）。
- **多维聚合统计**：支持按进程、域名、目标 IP、规则、策略组、节点、传输协议等维度与时间范围进行流量汇总。
- **待检查流量发现**：智能筛选 MATCH 兜底大流量、首次走代理进程、宽泛 UDP 代理、纯 IP 大流量等潜在可优化项。
- **监控覆盖率与缺口表达**：明确区分“正常监控”、“监控中断缺口”与“采集器未运行”，绝不把监控盲区伪装成“未识别流量”。
- **只读与本地优先**：仅作为旁路观察者读取 Mihomo 接口，不修改配置、不抓包解密、不触碰请求载荷。

---

## 文档索引

项目核心文档位于 `docs/` 目录：

- 📘 [产品需求与设计准则 (PRODUCT.md)](file:///E:/Projects/ProxyLens/ProxyLens/docs/PRODUCT.md) — 业务背景、用户痛点、十项产品原则、功能范围与验收场景。
- 🏗️ [系统架构与技术认知 (ARCHITECTURE.md)](file:///E:/Projects/ProxyLens/ProxyLens/docs/ARCHITECTURE.md) — 系统边界、数据流、Collector/UI 职责、技术约束与开放问题。
- 🗺️ [开发与验证路线图 (ROADMAP.md)](file:///E:/Projects/ProxyLens/ProxyLens/docs/ROADMAP.md) — 按验证顺序划分的 Phase 0 ~ Phase 4 规划与验收标准。
- 🧭 [项目实时状态与决策记录 (STATUS.md)](file:///E:/Projects/ProxyLens/ProxyLens/docs/STATUS.md) — 当前阶段、已确认基线、未决问题与下一步动作（后续 Agent 切换时首要参考）。

# ADR 0001: Collector Implementation Language Selection (Go vs Rust)

## Context

ProxyLens 作为一个面向 Windows + Mihomo 的本地旁路流量审计工具，其后台 Collector 组件需要以极轻量的资源占用（目标 CPU < 1%、RAM < 30MB）稳定运行。在 Phase 0 验证中，快照轮询候选周期为 250ms~1000ms（每秒 1~4 帧，活跃连接 50~1000 条）。

在 Phase 1 原型验证中，我们在 Windows 11 环境上对 Go (v1.24+) 状态机进行了离线 Spike 对照实测与 600s 真实生产 Soak 测试。

---

## Benchmark Evidence

### 1. 离线状态机 Spike 对照实测 (Offline Spike Benchmark):
基于 `tmp/phase1-language-spike/benchmark-report.json` 的实测数据：

| 指标 | Go (v1.24.11) | Rust (v1.96.0) | 比较与结论 |
| :--- | :---: | :---: | :--- |
| **Semantic Checksum** | 100% Match | 100% Match | **两个实现对全部状态机转换产生完全一致的 SHA256 签名** |
| **1x (500 帧) 吞吐量** | 4,072.8 帧/秒 | 12,933.1 帧/秒 | Rust 3.17x，两者均超目标需求 1,000x+ |
| **8x (4000 帧) 吞吐量** | 4,505.9 帧/秒 (32.6万 obs/s) | 15,086.1 帧/秒 (109.3万 obs/s) | Rust 3.35x |
| **单帧状态机处理耗时** | ~0.22 ms | ~0.06 ms | 在 250ms 快照间隔下，单帧开销占比均 < 0.1% |
| **GC / 内存稳定性** | 4次 GC / ~39MB alloc (500帧) | 0 GC / 极致内存受控 | 均能满足 Windows 后台轻量驻留需求 |

### 2. 生产路径实际 Windows 进程基准测试 (Measured Benchmark 3.0 & Soak):
详见脱敏凭证 `docs/benchmarks/phase1-collector-benchmark.json`：
- **250ms 快照周期**: 生产进程平均单核等效 CPU 占用仅需 **~0.13%**（整机 12 核占比 < 0.02%）；
- **内存驻留 (Working Set / RSS)**: 初始 **8.82 MB**，600 秒 (10分钟) Soak 峰值 **10.79 MB**，无明显线性增长；
- **队列与延迟**: 有界队列零丢帧、零无标记丢弃，完全满足产品轻量常驻目标。

---

## Windows Engineering Trade-offs

| 评估维度 | Go | Rust | 权衡分析 |
| :--- | :--- | :--- | :--- |
| **并发与状态机复杂度** | Goroutine + Channel + Context 天然解耦 Reader/Engine/Sink | Tokio Async + Arc/Mutex/Channel 所有权管理相对复杂 | **Go 显著降低并发状态机维护心智负担** |
| **Windows 单二进制构建** | 原生静态编译，可使用纯 Go SQLite (`modernc.org/sqlite`) 零 CGO | 静态编译优秀，但 C-bind SQLite 需要 MSVC 工具链 | **Go 在 Windows 上的免环境编译与部署极其便捷** |
| **网络与 WebSocket 生态** | 成熟稳定的 `gorilla/websocket` | 依赖 Tokio + Tungstenite 等 crate 组合 | 均可胜任 |
| **性能 Headroom** | 4,000+ 帧/秒（比 4 帧/秒高 1,000 倍） | 15,000 帧/秒（高 3,700 倍） | **Go 的算力余量已极大超出产品实际瓶颈** |

---

## Decision

**选择 Go 作为 ProxyLens 生产 Collector 的开发语言。**

### 核心决策理由：
1. **正确性与性能余量**：Go 在 250ms 高频快照下的状态机吞吐能力超过 4,000 帧/秒，CPU 占用极其微小（单核 < 0.3%），完全满足 <1% CPU 占用目标；
2. **工程与维护复杂度**：Goroutine、Channel 背压与 Context 取消机制极大简化了 Collector 断线重连、队列缓冲与状态机解耦的实现；
3. **Windows 纯 Go SQLite 生态**：避免引入复杂 CGO/MSVC 编译链，确保后续 Windows 单二进制打包极简稳定。

---

## Consequences

- 生产 Collector 代码统一组织在 `collector/` 目录下（Go module）；
- 统一使用 Go 1.24+ 标准工具链与 `gorilla/websocket` 依赖；
- 更新 `AGENTS.md` 中的构建、测试与运行命令；
- Spike 代码保留在 `tmp/phase1-language-spike/`，不污染生产目录。

# LeeClaw Roadmap

> 当前基线：**v0.10.1 — v0.10 Persistence & Integration Patch**

```text
v0.8  Skill + Knowledge Runtime                         ✅
v0.9  Retrieval Governance                             ✅
v0.10 Experience Promotion                             ✅ 当前基线
v0.11 Enterprise Persistence & HA                      🎯 下一版本
v0.12 Trace + Eval + CI                                📌
v1.0  Enterprise GA                                    🏁
```

## v0.10 已完成

Session / Experience / Evidence → Candidate → 来源追溯 → 去重/冲突/格式校验 → Admin Review → Promotion Gate → 发布前复核 → WeKnora Knowledge / OpenViking Shared Skill → Rollback。

核心门禁：Agent 无正式发布 Tool；personal candidate 必须显式提升到 Workspace；Knowledge 证据重新校验 KB Scope；Skill 必须有来源、Behavior Diff、通过 Eval，并执行 revision race check。

## v0.11 — Enterprise Persistence & HA

将当前单实例状态抽象替换为共享生产 Store，同时保持上层 Contract 不变：

```text
WorkspaceStore
SessionBindingStore
AuditStore
OntologyRegistry
PromotionStore
JobStore
```

生产实现建议 PostgreSQL；补齐事务、乐观锁、幂等、migration、Secret Provider、Job/Streaming、rate limit、quota、timeout、circuit breaker、backpressure 和 readiness。

验收重点：两个 Gateway 实例共享 Workspace/Session/Promotion 状态一致；并发审核发布不丢状态；节点重启不丢绑定；Secret 不进 Browser/Audit/Trace；Job 重试不重复创建下游资源。

## v0.12 — Trace + Eval + CI

统一 run_id / trace_id 贯穿 Skill Resolution、Memory/Experience、Retrieval、Evidence、Tool、Final Answer、Promotion。支持 deterministic replay 与 current-data re-run；建立 L1~L7 分层评测；PR/Release 自动执行完整构建、E2E、Eval regression、upstream compatibility 和 dirty check。

## v1.0 — Enterprise GA

以生产收敛为目标：安全、HA、migration、backup/restore、rollback、Trace/Eval/CI、升级兼容、运维文档全部真实验证后才进入 1.0.0。

## 长期工程铁律

1. **正确逻辑直接覆盖错误逻辑**：删除旧活跃路径，用唯一正确实现替换；历史 Release 可以保留，旧配置/门禁不能并存。
2. **每种资产一个 Source of Truth**：账号 OpenClaw；Knowledge WeKnora；Memory/Experience/Skill OpenViking；Ontology/Promotion Governance LeeClaw；实时事实业务系统。
3. **上游零侵入**：Public API/MCP → Plugin/Hook → Adapter → Derived Assembly，最后才考虑可退出的临时 Patch。
4. **浏览器不拥有可信身份**：profileId、Tenant、API Key、OpenViking Account、Knowledge Scope、Tool/Promotion Authority 都必须服务端解析。

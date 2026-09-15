# leeclaw-knowledge v0.10.1

OpenClaw 单一 Knowledge 导航 + WeKnora 原生 Knowledge 页面 + Agent Knowledge Runtime。

## 管理面

插件不再复制 Knowledge UI。认证后的 OpenClaw Profile 向服务端申请短时签名票据，经 `18791` 换取 HttpOnly Workspace 会话后嵌入 WeKnora 原生知识库页面。Knowledge Base、Document、Wiki、FAQ、Tag、Share 和 Entity Graph 均保留 WeKnora 原生交互；浏览器不能提交可信 Workspace、Tenant 或 API Key。

插件不注册 Memory、Skills 或 Workspaces 侧栏项；这些能力继续作为 OpenViking/Workspace Core 后端供 Agent Runtime 使用。

## Agent Runtime

注册两个 OpenClaw Tool：

```text
leeclaw_context_retrieve
leeclaw_context_get_evidence
```

Tool Factory 只从 OpenClaw `sessionKey/sessionId` 恢复 durable profile 和 Session-bound Workspace。模型参数中没有 Workspace、Tenant、User、Account、Credential。

`retrieve` 只能访问当前 Workspace 映射的 Knowledge Base Scope；`get_evidence` 读取 chunk 后会再次核验返回的 `knowledge_base_id`，防止已知 chunk id 绕过 Scope。

## Core API

`coreApiBaseUrl` 是 Graph + Runtime Context 的统一 LeeClaw Core API。OpenClaw Plugin 使用单独的 `LEECLAW_RUNTIME_TOKEN` 调 Runtime 路由，浏览器 CORS 不暴露任何 trusted runtime header。

## Workspace

页面 Workspace 是 profile 默认空间；Agent Session 第一次运行后固定 Workspace。页面随后切换 Workspace 不会改变既有 Agent Session，新建或 reset 后的新 Session 才采用新的默认空间。

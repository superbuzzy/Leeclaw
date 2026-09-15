function cleanUrl(value) { return String(value ?? "").trim().replace(/\/+$/u, ""); }
function optional(value) { const v = String(value ?? "").trim(); return v || undefined; }
function required(value, name) { const v = String(value ?? "").trim(); if (!v) throw new Error(`leeclaw-openviking: ${name} is required`); return v; }

export function resolveOpenVikingConfig(raw = {}) {
  const baseUrl = cleanUrl(raw.baseUrl ?? process.env.OPENVIKING_BASE_URL);
  if (!baseUrl) throw new Error("leeclaw-openviking: baseUrl is required");
  const timeout = Number(raw.requestTimeoutMs ?? process.env.LEECLAW_OPENVIKING_TIMEOUT_MS ?? 15000);
  const recallLimit = Number(raw.memoryRecallLimit ?? 8);
  const maxRecallChars = Number(raw.memoryMaxRecallChars ?? 12000);
  const commitPendingTokens = Number(raw.commitPendingTokens ?? 1200);
  const commitKeepRecentCount = Number(raw.commitKeepRecentCount ?? 6);
  const skillResolveLimit = Number(raw.skillResolveLimit ?? 5);
  const skillScoreThreshold = Number(raw.skillScoreThreshold ?? 0.62);
  const skillMaxContentChars = Number(raw.skillMaxContentChars ?? 20000);
  return {
    baseUrl,
    apiKey: optional(raw.apiKey ?? process.env.OPENVIKING_API_KEY),
    workspaceRegistryPath: required(raw.workspaceRegistryPath ?? process.env.LEECLAW_WORKSPACE_REGISTRY, "workspaceRegistryPath"),
    workspaceStatePath: optional(raw.workspaceStatePath ?? process.env.LEECLAW_WORKSPACE_STATE),
    memoryRuntimeEnabled: raw.memoryRuntimeEnabled !== false,
    skillRuntimeEnabled: raw.skillRuntimeEnabled !== false,
    skillResolveLimit: Number.isFinite(skillResolveLimit) ? Math.max(1, Math.min(Math.floor(skillResolveLimit), 20)) : 5,
    skillScoreThreshold: Number.isFinite(skillScoreThreshold) ? Math.max(0, Math.min(skillScoreThreshold, 1)) : 0.62,
    skillMaxContentChars: Number.isFinite(skillMaxContentChars) ? Math.max(2000, Math.min(Math.floor(skillMaxContentChars), 50000)) : 20000,
    memoryRecallLimit: Number.isFinite(recallLimit) ? Math.max(1, Math.min(recallLimit, 50)) : 8,
    memoryMaxRecallChars: Number.isFinite(maxRecallChars) ? Math.max(1000, Math.min(maxRecallChars, 50000)) : 12000,
    commitPendingTokens: Number.isFinite(commitPendingTokens) ? Math.max(0, Math.floor(commitPendingTokens)) : 1200,
    commitKeepRecentCount: Number.isFinite(commitKeepRecentCount) ? Math.max(0, Math.floor(commitKeepRecentCount)) : 6,
    requestTimeoutMs: Number.isFinite(timeout) ? Math.max(1000, Math.min(timeout, 60000)) : 15000,
  };
}

export class OpenVikingClient {
  constructor(config) { this.config = config; }
  headers(context = {}, jsonBody = false) {
    const headers = { Accept: "application/json" };
    if (jsonBody) headers["Content-Type"] = "application/json";
    if (this.config.apiKey) headers.Authorization = `Bearer ${this.config.apiKey}`;
    headers["X-OpenViking-Account"] = required(context.accountId, "trusted account");
    headers["X-OpenViking-User"] = required(context.userId, "trusted user");
    return headers;
  }
  async request(method, path, { body, context } = {}) {
    const controller = new AbortController(); const timer = setTimeout(() => controller.abort(), this.config.requestTimeoutMs);
    try {
      const response = await fetch(`${this.config.baseUrl}${path}`, { method, headers: this.headers(context, body !== undefined), body: body === undefined ? undefined : JSON.stringify(body), signal: controller.signal });
      const text = await response.text(); let payload = text;
      if (text) { try { payload = JSON.parse(text); } catch { /* keep text */ } }
      if (!response.ok) { const detail = typeof payload === "string" ? payload : JSON.stringify(payload); throw new Error(`OpenViking ${method} ${path} -> ${response.status}: ${detail.slice(0, 800)}`); }
      if (payload && typeof payload === "object" && "result" in payload) return payload.result;
      return payload;
    } finally { clearTimeout(timer); }
  }
  listSessions(context) { return this.request("GET", "/api/v1/sessions", { context }); }
  listMemory(context, limit = 100) { const q = new URLSearchParams({ uri: "viking://~/memories", recursive: "true", output: "agent", node_limit: String(limit), show_all_hidden: "false", include_tags: "true" }); return this.request("GET", `/api/v1/fs/ls?${q}`, { context }); }
  searchMemory(context, query, limit = 20) { return this.request("POST", "/api/v1/search/find", { context, body: { query, target_uri: "viking://~/memories", limit, read_content: true } }); }
  readContent(context, uri) { const q = new URLSearchParams({ uri, raw: "false" }); return this.request("GET", `/api/v1/content/read?${q}`, { context }); }
  statContent(context, uri) { const q = new URLSearchParams({ uri }); return this.request("GET", `/api/v1/fs/stat?${q}`, { context }); }
  readAttrs(context, uri) { const q = new URLSearchParams({ uri }); return this.request("GET", `/api/v1/fs/attrs?${q}`, { context }); }
  writeContent(context, uri, content, mode = "replace", tags = []) { return this.request("POST", "/api/v1/content/write", { context, body: { uri, content, mode, wait: true, tags, tag_mode: "replace" } }); }
  deleteContent(context, uri) { const q = new URLSearchParams({ uri, recursive: "false", wait: "true" }); return this.request("DELETE", `/api/v1/fs?${q}`, { context }); }
  listSkills(context) { return this.request("GET", "/api/v1/skills", { context }); }
  findSkills(context, query, limit = 20, scoreThreshold) { return this.request("POST", "/api/v1/skills/find", { context, body: { query, limit, level: [0, 1], ...(Number.isFinite(scoreThreshold) ? { score_threshold: scoreThreshold } : {}) } }); }
  getSkill(context, name, targetUri) { const q = new URLSearchParams({ level: "2", include_content: "true", include_files: "true" }); if (targetUri) q.set("target_uri", targetUri); return this.request("GET", `/api/v1/skills/${encodeURIComponent(name)}?${q}`, { context }); }
  getSession(context, sessionId) { return this.request("GET", `/api/v1/sessions/${encodeURIComponent(sessionId)}`, { context }); }
  getSessionArchive(context, sessionId, archiveId) { return this.request("GET", `/api/v1/sessions/${encodeURIComponent(sessionId)}/archives/${encodeURIComponent(archiveId)}`, { context }); }
  addSessionMessage(context, sessionId, role, text) { return this.request("POST", `/api/v1/sessions/${encodeURIComponent(sessionId)}/messages`, { context, body: { role, parts: [{ type: "text", text }] } }); }
  commitSession(context, sessionId, keepRecentCount = 0) { return this.request("POST", `/api/v1/sessions/${encodeURIComponent(sessionId)}/commit`, { context, body: keepRecentCount > 0 ? { keep_recent_count: keepRecentCount } : {} }); }
}

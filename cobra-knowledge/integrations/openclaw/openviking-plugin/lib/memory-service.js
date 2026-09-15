import { randomUUID } from "node:crypto";

const MEMORY_TYPES = new Set(["profile", "preferences", "entities", "events", "cases", "trajectories", "experiences"]);
const WRITABLE_TYPES = new Set(["profile", "preferences", "entities", "events", "cases"]);

function boundedLimit(value, fallback = 50) {
  const n = Number(value);
  return Number.isFinite(n) ? Math.max(1, Math.min(Math.floor(n), 500)) : fallback;
}

function cleanType(value) {
  const type = String(value ?? "events").trim().toLowerCase();
  if (!MEMORY_TYPES.has(type)) throw new Error(`leeclaw-memory: unsupported memory type ${type}`);
  return type;
}

function cleanText(value, name) {
  const text = String(value ?? "").trim();
  if (!text) throw new Error(`leeclaw-memory: ${name} is required`);
  if (text.length > 100_000) throw new Error(`leeclaw-memory: ${name} is too large`);
  return text;
}

function safeMemoryUri(value) {
  const uri = String(value ?? "").trim();
  if (!uri.startsWith("viking://~/memories/") || uri.includes("..") || /[?#]/u.test(uri)) {
    throw new Error("leeclaw-memory: memory id is outside the authenticated user's memory namespace");
  }
  return uri;
}

function bodyWithMetadata(content, type, source = "openclaw-memory-ui") {
  const metadata = JSON.stringify({ memory_type: type, source, version: 1, updated_at: new Date().toISOString() }, null, 2);
  return `${content}\n\n<!-- MEMORY_FIELDS\n${metadata}\n-->`;
}

function unwrapRows(value) {
  if (Array.isArray(value)) return value;
  if (!value || typeof value !== "object") return [];
  for (const key of ["items", "memories", "results", "resources", "children", "entries"]) if (Array.isArray(value[key])) return value[key];
  return [];
}

export class LeeClawMemoryService {
  constructor(client) { this.client = client; }
  async listMemories(principal, input = {}) {
    const type = input.type ? cleanType(input.type) : undefined;
    const result = await this.client.listMemory(principal, boundedLimit(input.limit, 100));
    const items = unwrapRows(result).filter((item) => !type || String(item?.uri ?? "").includes(`/memories/${type}/`));
    return { items, backend: "openviking" };
  }
  searchMemories(principal, input = {}) { return this.client.searchMemory(principal, String(input.query ?? ""), boundedLimit(input.limit, 20)); }
  async getMemory(principal, input = {}) {
    const uri = safeMemoryUri(input.memoryId ?? input.uri);
    const [content, stat, attrs] = await Promise.all([this.client.readContent(principal, uri), this.client.statContent(principal, uri), this.client.readAttrs(principal, uri)]);
    return { id: uri, uri, content, stat, attrs, source: { system: "openviking", uri, workspaceId: principal.workspaceId, accountId: principal.accountId, userId: principal.userId } };
  }
  async createMemory(principal, input = {}) {
    const type = cleanType(input.type);
    if (!WRITABLE_TYPES.has(type)) throw new Error(`leeclaw-memory: ${type} is extraction-owned and cannot be created manually`);
    const uri = `viking://~/memories/${type}/manual-${randomUUID()}.md`;
    await this.client.writeContent(principal, uri, bodyWithMetadata(cleanText(input.content, "content"), type), "create", [`leeclaw:type=${type}`, "leeclaw:source=openclaw-memory-ui"]);
    return this.getMemory(principal, { uri });
  }
  async updateMemory(principal, input = {}) {
    const uri = safeMemoryUri(input.memoryId ?? input.uri);
    const type = cleanType(input.type ?? uri.split("/memories/")[1]?.split("/")[0]);
    if (!WRITABLE_TYPES.has(type)) throw new Error(`leeclaw-memory: ${type} is extraction-owned and cannot be edited manually`);
    await this.client.writeContent(principal, uri, bodyWithMetadata(cleanText(input.content, "content"), type), "replace", [`leeclaw:type=${type}`, "leeclaw:source=openclaw-memory-ui"]);
    return this.getMemory(principal, { uri });
  }
  async deleteMemory(principal, input = {}) { const uri = safeMemoryUri(input.memoryId ?? input.uri); await this.client.deleteContent(principal, uri); return { deleted: true, id: uri, uri }; }
  listSessions(principal) { return this.client.listSessions(principal); }
  getSession(principal, input = {}) { return this.client.getSession(principal, cleanText(input.sessionId, "sessionId")); }
  async searchArchive(principal, input = {}) { const result = await this.client.searchMemory(principal, String(input.query ?? ""), boundedLimit(input.limit, 20)); return { ...((result && typeof result === "object") ? result : { items: result }), sourceKind: "session-archive" }; }
  getArchive(principal, input = {}) { return this.client.getSessionArchive(principal, cleanText(input.sessionId, "sessionId"), cleanText(input.archiveId, "archiveId")); }
  async getMemorySource(principal, input = {}) { return (await this.getMemory(principal, input)).source; }
}

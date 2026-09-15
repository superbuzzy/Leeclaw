import test from "node:test";
import assert from "node:assert/strict";
import { LeeClawMemoryService } from "../lib/memory-service.js";

const principal = { workspaceId: "ws", accountId: "account", userId: "profile" };

function fakeClient() {
  const calls = [];
  return {
    calls,
    listMemory: async (...args) => { calls.push(["listMemory", ...args]); return { items: [{ uri: "viking://~/memories/events/a.md" }, { uri: "viking://~/memories/preferences/b.md" }] }; },
    searchMemory: async (...args) => { calls.push(["searchMemory", ...args]); return { memories: [] }; },
    readContent: async (...args) => { calls.push(["readContent", ...args]); return "body"; },
    statContent: async (...args) => { calls.push(["statContent", ...args]); return { size: 4 }; },
    readAttrs: async (...args) => { calls.push(["readAttrs", ...args]); return { attrs: { memory: { memory_type: "events" } } }; },
    writeContent: async (...args) => { calls.push(["writeContent", ...args]); return {}; },
    deleteContent: async (...args) => { calls.push(["deleteContent", ...args]); return {}; },
    listSessions: async () => [], getSession: async () => ({}), getSessionArchive: async () => ({}),
  };
}

test("listMemories filters within OpenViking memory types", async () => {
  const client = fakeClient(); const service = new LeeClawMemoryService(client);
  const result = await service.listMemories(principal, { type: "events", limit: 10 });
  assert.deepEqual(result.items.map((x) => x.uri), ["viking://~/memories/events/a.md"]);
  assert.equal(result.backend, "openviking");
});

test("manual memory CRUD stays in the authenticated user namespace", async () => {
  const client = fakeClient(); const service = new LeeClawMemoryService(client);
  const created = await service.createMemory(principal, { type: "preferences", content: "Prefers concise reports." });
  assert.match(created.uri, /^viking:\/\/~\/memories\/preferences\/manual-[0-9a-f-]+\.md$/u);
  const write = client.calls.find((call) => call[0] === "writeContent");
  assert.equal(write[1], principal);
  assert.match(write[3], /"memory_type": "preferences"/u);
  await assert.rejects(() => service.getMemory(principal, { uri: "viking://resources/secret.md" }), /outside/u);
  await assert.rejects(() => service.updateMemory(principal, { uri: "viking://~/memories/experiences/x.md", content: "x" }), /extraction-owned/u);
});

test("memory source is server-derived", async () => {
  const service = new LeeClawMemoryService(fakeClient());
  const source = await service.getMemorySource(principal, { uri: "viking://~/memories/events/a.md" });
  assert.deepEqual(source, { system: "openviking", uri: "viking://~/memories/events/a.md", workspaceId: "ws", accountId: "account", userId: "profile" });
});

import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { OpenVikingClient, resolveOpenVikingConfig } from "./lib/client.js";
import { registerLeeClawAgentRuntime } from "./lib/agent-runtime.js";
import { openVikingPrincipal } from "./lib/principal.js";
import { WorkspaceRegistry } from "@leeclaw/workspace-core";
import { LeeClawMemoryService } from "./lib/memory-service.js";

function registerMethod(api, name, scope, handler) {
  api.registerGatewayMethod(name, async (options) => {
    try { options.respond(true, await handler(options)); }
    catch (error) { options.respond(false, { error: error instanceof Error ? error.message : String(error) }); }
  }, { scope, profileAccess: "required" });
}
function requestedWorkspace(options) { return String(options.params?.workspaceId ?? "").trim() || undefined; }

export default definePluginEntry({
  id: "leeclaw-openviking",
  name: "LeeClaw Memory & Skills",
  description: "OpenClaw-native multi-workspace Memory and Skill integration backed by OpenViking.",
  register(api) {
    const config = resolveOpenVikingConfig(api.pluginConfig ?? {});
    const workspaces = new WorkspaceRegistry(config.workspaceRegistryPath, config.workspaceStatePath);
    const client = new OpenVikingClient(config);
    const memory = new LeeClawMemoryService(client);
    const principal = (options) => openVikingPrincipal(options.client, workspaces, requestedWorkspace(options));

    api.session.controls.registerControlUiDescriptor({ surface: "tab", id: "memory", label: "Memory", icon: "brain", group: "control", requiredScopes: ["operator.read"] });
    api.session.controls.registerControlUiDescriptor({ surface: "tab", id: "skills", label: "Skills", icon: "puzzle", group: "control", requiredScopes: ["operator.read"] });

    registerMethod(api, "leeclaw.memory.workspace", "operator.read", (options) => {
      const p = principal(options); return { id: p.workspaceId, name: p.workspaceName, role: p.workspaceRole, userId: p.userId };
    });
    registerMethod(api, "leeclaw.memory.list", "operator.read", (options) => memory.listMemories(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.search", "operator.read", (options) => memory.searchMemories(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.get", "operator.read", (options) => memory.getMemory(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.create", "operator.write", (options) => memory.createMemory(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.update", "operator.write", (options) => memory.updateMemory(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.delete", "operator.write", (options) => memory.deleteMemory(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.sessions", "operator.read", (options) => memory.listSessions(principal(options)));
    registerMethod(api, "leeclaw.memory.session", "operator.read", (options) => memory.getSession(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.archive.search", "operator.read", (options) => memory.searchArchive(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.archive.get", "operator.read", (options) => memory.getArchive(principal(options), options.params));
    registerMethod(api, "leeclaw.memory.source", "operator.read", (options) => memory.getMemorySource(principal(options), options.params));
    registerMethod(api, "leeclaw.skills.list", "operator.read", (options) => client.listSkills(principal(options)));
    registerMethod(api, "leeclaw.skills.find", "operator.read", (options) => client.findSkills(principal(options), String(options.params.query ?? ""), Number(options.params.limit ?? 20)));
    registerMethod(api, "leeclaw.skills.get", "operator.read", (options) => client.getSkill(principal(options), String(options.params.name ?? ""), options.params.targetUri ? String(options.params.targetUri) : undefined));
    registerMethod(api, "leeclaw.skills.workspace", "operator.read", (options) => ({ ...principal(options) }));

    registerLeeClawAgentRuntime(api, config, client, workspaces);
    api.logger.info("leeclaw-openviking: v0.10 OpenViking Memory adapter + Skill runtime enabled");
  },
});

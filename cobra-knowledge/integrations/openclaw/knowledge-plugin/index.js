import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { createHmac, randomBytes } from "node:crypto";
import { AuditLog } from "./lib/audit-log.js";
import { createKnowledgeRuntimeTool } from "./lib/agent-tools.js";
import { ContextRuntimeClient } from "./lib/context-runtime-client.js";
import { resolveKnowledgeConfig } from "./lib/config.js";
import { knowledgePrincipal, requireDurableProfileId } from "./lib/principal.js";
import { WeKnoraClient } from "./lib/weknora-client.js";
import { WorkspaceRegistry } from "@leeclaw/workspace-core";

function requiredString(params, key) {
  const value = String(params?.[key] ?? "").trim();
  if (!value) throw new Error(`${key} is required`);
  return value;
}

function requestedWorkspace(options) {
  return String(options.params?.workspaceId ?? "").trim() || undefined;
}

function registerMethod(api, name, scope, handler) {
  api.registerGatewayMethod(name, async (options) => {
    try { options.respond(true, await handler(options)); }
    catch (error) { options.respond(false, { error: error instanceof Error ? error.message : String(error) }); }
  }, { scope, profileAccess: "required" });
}

function resourceMeta(params = {}, defaults = {}) {
  const idKey = String(defaults.idKey ?? "").trim();
  const resourceId = defaults.resourceId ?? (idKey ? params[idKey] : undefined) ?? params.kbId ?? params.id;
  return {
    resourceType: defaults.resourceType,
    resourceId: String(resourceId ?? "").trim() || undefined,
    detail: {
      role: params.role,
      permission: params.permission,
      name: params.name,
      targetProfileId: params.profileId,
      organizationId: params.organizationId,
      targetWorkspaceId: params.workspaceId,
      knowledgeBaseId: params.kbId,
    },
  };
}

export default definePluginEntry({
  id: "leeclaw-knowledge",
  name: "LeeClaw Knowledge",
  description: "OpenClaw-native multi-workspace knowledge management backed by WeKnora and LeeClaw Ontology Registry.",
  register(api) {
    const config = resolveKnowledgeConfig(api.pluginConfig ?? {});
    const workspaces = new WorkspaceRegistry(config.workspaceRegistryPath, config.workspaceStatePath);
    const audit = new AuditLog(config.auditLogPath);
    const client = new WeKnoraClient(config);
    const contextClient = new ContextRuntimeClient(config);
    const profileId = (options) => requireDurableProfileId(options.client);
    const principal = (options, minimumRole = "viewer") => knowledgePrincipal(options.client, workspaces, requestedWorkspace(options), minimumRole);

    for (const toolName of ["leeclaw_context_retrieve", "leeclaw_context_get_evidence"]) {
      api.registerTool((toolContext) => createKnowledgeRuntimeTool(api, workspaces, contextClient, toolContext, toolName), { name: toolName, optional: true });
    }

    const record = (event) => {
      try { audit.record(event); }
      catch (error) { api.logger.warn(`leeclaw-audit: write skipped: ${error instanceof Error ? error.message : String(error)}`); }
    };

    const mutate = async (options, minimumRole, action, defaults, operation) => {
      const ctx = principal(options, minimumRole);
      const meta = resourceMeta(options.params, defaults);
      try {
        const result = await operation(ctx);
        record({ actorProfileId: ctx.userId, workspaceId: ctx.workspaceId, action, outcome: "success", ...meta });
        return result;
      } catch (error) {
        record({ actorProfileId: ctx.userId, workspaceId: ctx.workspaceId, action, outcome: "failure", error: error instanceof Error ? error.message : String(error), ...meta });
        throw error;
      }
    };

    api.session.controls.registerControlUiDescriptor({
      surface: "tab", id: "knowledge", label: "Knowledge",
      description: "Knowledge bases, documents, Wiki, FAQ, graph, sharing and workspace access.",
      icon: "bookOpen", group: "control", requiredScopes: ["operator.read"],
    });
    registerMethod(api, "leeclaw.knowledge.webSession", "operator.read", (options) => {
      const ctx = principal(options, "viewer");
      const payload = Buffer.from(JSON.stringify({
        profileId: ctx.userId,
        workspaceId: ctx.workspaceId,
        exp: Math.floor(Date.now() / 1000) + 60,
        nonce: randomBytes(12).toString("base64url"),
      })).toString("base64url");
      const signature = createHmac("sha256", config.runtimeToken).update(payload).digest("base64url");
      return { ticket: `${payload}.${signature}`, expiresIn: 60 };
    });

    registerMethod(api, "leeclaw.workspaces.list", "operator.read", (options) => ({ items: workspaces.listFor(profileId(options)) }));
    registerMethod(api, "leeclaw.workspaces.current", "operator.read", (options) => {
      const workspace = workspaces.resolve(profileId(options));
      return { id: workspace.id, name: workspace.name, description: workspace.description, role: workspace.role, userId: profileId(options) };
    });
    registerMethod(api, "leeclaw.workspaces.switch", "operator.write", (options) => {
      const actor = profileId(options);
      const workspace = workspaces.switch(actor, requiredString(options.params, "workspaceId"));
      record({ actorProfileId: actor, workspaceId: workspace.id, action: "workspace.switch", outcome: "success", resourceType: "workspace", resourceId: workspace.id });
      return { id: workspace.id, name: workspace.name, description: workspace.description, role: workspace.role, userId: actor };
    });
    registerMethod(api, "leeclaw.workspaces.members", "operator.read", (options) => ({ items: workspaces.listMembers(profileId(options), requestedWorkspace(options)) }));
    registerMethod(api, "leeclaw.workspaces.member.upsert", "operator.admin", (options) => {
      const actor = profileId(options);
      const workspaceId = requiredString(options.params, "workspaceId");
      const result = workspaces.upsertMember(actor, workspaceId, requiredString(options.params, "profileId"), requiredString(options.params, "role"), String(options.params.displayName ?? ""));
      record({ actorProfileId: actor, workspaceId, action: "workspace.member.upsert", outcome: "success", resourceType: "workspace_member", resourceId: result.profileId, detail: { role: result.role, targetProfileId: result.profileId } });
      return result;
    });
    registerMethod(api, "leeclaw.workspaces.member.remove", "operator.admin", (options) => {
      const actor = profileId(options);
      const workspaceId = requiredString(options.params, "workspaceId");
      const target = requiredString(options.params, "profileId");
      const result = workspaces.removeMember(actor, workspaceId, target);
      record({ actorProfileId: actor, workspaceId, action: "workspace.member.remove", outcome: "success", resourceType: "workspace_member", resourceId: target, detail: { targetProfileId: target } });
      return result;
    });

    registerMethod(api, "leeclaw.audit.list", "operator.read", (options) => {
      const ctx = principal(options, "viewer");
      return { items: audit.list(ctx.workspaceId, { limit: options.params?.limit, resourceId: options.params?.resourceId }) };
    });

    registerMethod(api, "leeclaw.knowledge.health", "operator.read", async (options) => {
      const ctx = principal(options);
      await client.health(ctx);
      return { ok: true, workspaceId: ctx.workspaceId, workspaceRole: ctx.workspaceRole, userId: ctx.userId, weknora: ctx.weknoraBaseUrl ?? config.weknoraBaseUrl, core: config.coreApiBaseUrl };
    });
    registerMethod(api, "leeclaw.knowledge.list", "operator.read", (options) => client.listKnowledgeBases(principal(options), String(options.params.creator ?? "all")));
    registerMethod(api, "leeclaw.knowledge.get", "operator.read", (options) => client.getKnowledgeBase(principal(options), requiredString(options.params, "id")));
    registerMethod(api, "leeclaw.knowledge.create", "operator.write", (options) => mutate(options, "editor", "knowledge_base.create", { resourceType: "knowledge_base" }, (ctx) => client.createKnowledgeBase(ctx, options.params)));
    registerMethod(api, "leeclaw.knowledge.update", "operator.write", (options) => mutate(options, "editor", "knowledge_base.update", { resourceType: "knowledge_base" }, (ctx) => client.updateKnowledgeBase(ctx, requiredString(options.params, "id"), options.params)));
    registerMethod(api, "leeclaw.knowledge.delete", "operator.admin", (options) => mutate(options, "admin", "knowledge_base.delete", { resourceType: "knowledge_base" }, (ctx) => client.deleteKnowledgeBase(ctx, requiredString(options.params, "id"))));

    registerMethod(api, "leeclaw.knowledge.documents", "operator.read", (options) => client.listDocuments(principal(options), requiredString(options.params, "kbId"), options.params));
    registerMethod(api, "leeclaw.knowledge.document.upload", "operator.write", (options) => mutate(options, "editor", "document.upload", { resourceType: "knowledge_base" }, (ctx) => client.uploadDocument(ctx, requiredString(options.params, "kbId"), options.params)));
    registerMethod(api, "leeclaw.knowledge.document.url", "operator.write", (options) => mutate(options, "editor", "document.ingest_url", { resourceType: "knowledge_base" }, (ctx) => client.createKnowledgeFromURL(ctx, requiredString(options.params, "kbId"), options.params)));
    registerMethod(api, "leeclaw.knowledge.document.manual", "operator.write", (options) => mutate(options, "editor", "document.create_manual", { resourceType: "knowledge_base" }, (ctx) => client.createManualKnowledge(ctx, requiredString(options.params, "kbId"), options.params)));
    registerMethod(api, "leeclaw.knowledge.document.delete", "operator.write", (options) => mutate(options, "editor", "document.delete", { resourceType: "document", idKey: "documentId" }, (ctx) => client.deleteDocument(ctx, requiredString(options.params, "documentId"))));
    registerMethod(api, "leeclaw.knowledge.document.reparse", "operator.write", (options) => mutate(options, "editor", "document.reparse", { resourceType: "document", idKey: "documentId" }, (ctx) => client.reparseDocument(ctx, requiredString(options.params, "documentId"), options.params.processConfig)));
    registerMethod(api, "leeclaw.knowledge.document.cancel", "operator.write", (options) => mutate(options, "editor", "document.cancel_parse", { resourceType: "document", idKey: "documentId" }, (ctx) => client.cancelDocumentParse(ctx, requiredString(options.params, "documentId"))));
    registerMethod(api, "leeclaw.knowledge.folders", "operator.read", (options) => client.listFolders(principal(options), requiredString(options.params, "kbId")));

    registerMethod(api, "leeclaw.knowledge.tags", "operator.read", (options) => client.listTags(principal(options), requiredString(options.params, "kbId"), options.params));
    registerMethod(api, "leeclaw.knowledge.tag.create", "operator.write", (options) => mutate(options, "editor", "tag.create", { resourceType: "knowledge_base" }, (ctx) => client.createTag(ctx, requiredString(options.params, "kbId"), options.params)));
    registerMethod(api, "leeclaw.knowledge.tag.update", "operator.write", (options) => mutate(options, "editor", "tag.update", { resourceType: "tag", idKey: "tagId" }, (ctx) => client.updateTag(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "tagId"), options.params.patch ?? {})));
    registerMethod(api, "leeclaw.knowledge.tag.delete", "operator.write", (options) => mutate(options, "editor", "tag.delete", { resourceType: "tag", idKey: "tagId" }, (ctx) => client.deleteTag(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "tagId"), Boolean(options.params.force))));

    registerMethod(api, "leeclaw.knowledge.faq", "operator.read", (options) => client.listFAQ(principal(options), requiredString(options.params, "kbId"), options.params));
    registerMethod(api, "leeclaw.knowledge.faq.create", "operator.write", (options) => mutate(options, "editor", "faq.create", { resourceType: "knowledge_base" }, (ctx) => client.createFAQ(ctx, requiredString(options.params, "kbId"), options.params.entry ?? options.params)));
    registerMethod(api, "leeclaw.knowledge.faq.update", "operator.write", (options) => mutate(options, "editor", "faq.update", { resourceType: "faq", idKey: "entryId" }, (ctx) => client.updateFAQ(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "entryId"), options.params.entry ?? {})));
    registerMethod(api, "leeclaw.knowledge.faq.delete", "operator.write", (options) => mutate(options, "editor", "faq.delete", { resourceType: "knowledge_base", resourceId: String(options.params.kbId ?? "") }, (ctx) => client.deleteFAQ(ctx, requiredString(options.params, "kbId"), options.params.ids)));

    registerMethod(api, "leeclaw.knowledge.wiki", "operator.read", (options) => client.listWikiPages(principal(options), requiredString(options.params, "kbId"), options.params));
    registerMethod(api, "leeclaw.knowledge.wiki.create", "operator.write", (options) => mutate(options, "editor", "wiki.create", { resourceType: "knowledge_base" }, (ctx) => client.createWikiPage(ctx, requiredString(options.params, "kbId"), options.params.page ?? options.params)));
    registerMethod(api, "leeclaw.knowledge.wiki.update", "operator.write", (options) => mutate(options, "editor", "wiki.update", { resourceType: "wiki", idKey: "slug" }, (ctx) => client.updateWikiPage(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "slug"), options.params.page ?? {})));
    registerMethod(api, "leeclaw.knowledge.wiki.delete", "operator.write", (options) => mutate(options, "editor", "wiki.delete", { resourceType: "wiki", idKey: "slug" }, (ctx) => client.deleteWikiPage(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "slug"))));

    registerMethod(api, "leeclaw.knowledge.search", "operator.read", (options) => client.hybridSearch(principal(options), requiredString(options.params, "kbId"), options.params.query ?? options.params));
    registerMethod(api, "leeclaw.knowledge.organizations", "operator.read", (options) => client.listOrganizations(principal(options)));
    registerMethod(api, "leeclaw.knowledge.shares", "operator.read", (options) => client.listShares(principal(options), requiredString(options.params, "kbId")));
    registerMethod(api, "leeclaw.knowledge.share.create", "operator.admin", (options) => mutate(options, "admin", "share.create", { resourceType: "knowledge_base", resourceId: String(options.params.kbId ?? "") }, (ctx) => client.createShare(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "organizationId"), options.params.permission)));
    registerMethod(api, "leeclaw.knowledge.share.update", "operator.admin", (options) => mutate(options, "admin", "share.update", { resourceType: "knowledge_base", resourceId: String(options.params.kbId ?? "") }, (ctx) => client.updateShare(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "shareId"), options.params.permission)));
    registerMethod(api, "leeclaw.knowledge.share.delete", "operator.admin", (options) => mutate(options, "admin", "share.delete", { resourceType: "knowledge_base", resourceId: String(options.params.kbId ?? "") }, (ctx) => client.deleteShare(ctx, requiredString(options.params, "kbId"), requiredString(options.params, "shareId"))));
    registerMethod(api, "leeclaw.knowledge.graph", "operator.read", (options) => client.graph(principal(options), requiredString(options.params, "kbId"), String(options.params.view ?? "entity")));
  },
});

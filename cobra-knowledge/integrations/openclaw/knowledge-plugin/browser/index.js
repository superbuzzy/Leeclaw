import { defineControlUiPlugin } from "openclaw/plugin-sdk/control-ui";
import "./knowledge.css";

function knowledgeOrigin() {
  const protocol = window.location.protocol;
  const host = window.location.hostname;
  const configured = Number(window.__LEECLAW_KNOWLEDGE_PORT__ ?? 18791);
  return `${protocol}//${host}:${configured}`;
}

function createKnowledgePage(host) {
  return (container) => {
    const root = document.createElement("section");
    root.className = "leeclaw-knowledge-native";
    const status = document.createElement("div");
    status.className = "leeclaw-knowledge-loading";
    status.textContent = "正在连接 Knowledge…";
    root.append(status);
    container.append(root);
    let disposed = false;

    (async () => {
      try {
        const session = await host.request("leeclaw.knowledge.webSession", {});
        if (disposed) return;
        // Non-secret sentinel: the 18791 gateway removes it and injects the
        // server-side credential derived from the authenticated workspace.
        localStorage.setItem("weknora_token", "leeclaw-workspace-session");
        localStorage.setItem("weknora_lite_mode", "false");
        const frame = document.createElement("iframe");
        frame.className = "leeclaw-knowledge-frame";
        frame.title = "Knowledge";
        frame.allow = "clipboard-read; clipboard-write";
        const next = "/platform/knowledge-bases?leeclaw_embed=1";
        frame.src = `${knowledgeOrigin()}/auth/session?ticket=${encodeURIComponent(session.ticket)}&next=${encodeURIComponent(next)}`;
        frame.addEventListener("load", () => status.remove(), { once: true });
        root.append(frame);
      } catch (error) {
        status.className = "leeclaw-knowledge-error";
        status.textContent = `Knowledge 连接失败：${error instanceof Error ? error.message : String(error)}`;
      }
    })();

    return { dispose() { disposed = true; root.remove(); } };
  };
}

export default defineControlUiPlugin({
  id: "leeclaw-knowledge",
  activate(host) {
    const disposePage = host.ui.registerPage({ id: "knowledge", label: "Knowledge", mount: createKnowledgePage(host) });
    const disposeNavigation = host.ui.registerNavigation({ id: "knowledge", label: "Knowledge", page: { id: "knowledge" }, icon: "bookOpen", order: 30 });
    return () => { disposeNavigation(); disposePage(); };
  },
});

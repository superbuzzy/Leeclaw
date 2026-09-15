import { defineControlUiPlugin } from "openclaw/plugin-sdk/control-ui";
import "./openviking.css";

function el(tag, cls, text) { const n = document.createElement(tag); if (cls) n.className = cls; if (text !== undefined) n.textContent = text; return n; }
function unwrapItems(value, keys) {
  if (Array.isArray(value)) return value;
  if (!value || typeof value !== "object") return [];
  for (const key of keys) if (Array.isArray(value[key])) return value[key];
  return [];
}
function label(item) { return String(item?.name ?? item?.title ?? item?.session_id ?? item?.id ?? item?.uri ?? "未命名"); }

function renderList(container, rows, onSelect) {
  container.replaceChildren();
  if (!rows.length) { container.append(el("div", "lov-empty", "暂无数据")); return; }
  for (const item of rows) {
    const row = el(onSelect ? "button" : "div", "lov-row");
    row.append(el("strong", "lov-row-title", label(item)));
    const sub = item?.description ?? item?.abstract ?? item?.uri ?? item?.updated_at ?? item?.created_at;
    if (sub) row.append(el("span", "lov-row-sub", String(sub).slice(0, 240)));
    if (onSelect) row.onclick = () => onSelect(item);
    container.append(row);
  }
}

function memoryMount(host) {
  return (container) => {
    const root = el("section", "lov-page");
    root.append(el("h1", "lov-title", "Memory"), el("p", "lov-subtitle", "OpenViking Session、Memory 与 Experience 的统一入口。"));
    const search = el("div", "lov-search");
    const input = el("input", "lov-input"); input.placeholder = "搜索长期记忆…";
    const button = el("button", "lov-button", "搜索");
    const sessionsButton = el("button", "lov-button lov-secondary", "最近会话");
    search.append(input, button, sessionsButton);
    const body = el("div", "lov-list");
    root.append(search, body); container.append(root);
    button.onclick = async () => {
      body.replaceChildren(el("div", "lov-empty", "搜索中…"));
      try {
        const result = await host.request("leeclaw.memory.search", { query: input.value, limit: 30 });
        const rows = unwrapItems(result, ["memories", "items", "results", "resources"]);
        renderList(body, rows);
      } catch (e) { body.replaceChildren(el("div", "lov-error", String(e))); }
    };
    sessionsButton.onclick = async () => {
      body.replaceChildren(el("div", "lov-empty", "加载中…"));
      try {
        const result = await host.request("leeclaw.memory.sessions", {});
        const rows = unwrapItems(result, ["sessions", "items"]);
        renderList(body, rows);
      } catch (e) { body.replaceChildren(el("div", "lov-error", String(e))); }
    };
    sessionsButton.click();
    return { dispose: () => root.remove() };
  };
}

function skillMount(host) {
  return (container) => {
    const root = el("section", "lov-page");
    root.append(el("h1", "lov-title", "Skills"), el("p", "lov-subtitle", "Skill 的权威资产保存在 OpenViking；OpenClaw 只负责发现、读取和执行。"));
    const search = el("div", "lov-search");
    const input = el("input", "lov-input"); input.placeholder = "按任务语义查找 Skill…";
    const find = el("button", "lov-button", "语义查找");
    const all = el("button", "lov-button lov-secondary", "全部 Skill");
    search.append(input, find, all);
    const layout = el("div", "lov-skill-layout");
    const list = el("div", "lov-list");
    const detail = el("pre", "lov-detail", "选择一个 Skill 查看 SKILL.md。 ");
    layout.append(list, detail); root.append(search, layout); container.append(root);
    async function show(item) {
      detail.textContent = "加载中…";
      try {
        const targetUri = item.root_uri?.includes("/skills/") ? item.root_uri.replace(/\/[^/]+$/u, "") : item.root_uri;
        const data = await host.request("leeclaw.skills.get", { name: item.name, targetUri });
        detail.textContent = data?.content ?? data?.skill_md ?? data?.skill_md_content ?? JSON.stringify(data, null, 2);
      } catch (e) { detail.textContent = String(e); }
    }
    async function load(method, params) {
      list.replaceChildren(el("div", "lov-empty", "加载中…"));
      try {
        const result = await host.request(method, params);
        const rows = unwrapItems(result, ["skills", "items"]);
        renderList(list, rows, show);
      } catch (e) { list.replaceChildren(el("div", "lov-error", String(e))); }
    }
    find.onclick = () => load("leeclaw.skills.find", { query: input.value, limit: 30 });
    all.onclick = () => load("leeclaw.skills.list", {});
    all.click();
    return { dispose: () => root.remove() };
  };
}

export default defineControlUiPlugin({
  id: "leeclaw-openviking",
  activate() { return () => {}; },
});

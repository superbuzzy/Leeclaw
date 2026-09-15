import { createRequire } from "node:module";
import fs from "node:fs/promises";

const require = createRequire("/app/openclaw.mjs");
const { chromium } = require("playwright-core");

const password = process.env.OPENCLAW_GATEWAY_TOKEN;
if (!password) throw new Error("OPENCLAW_GATEWAY_TOKEN is required");

const baseURL = process.env.LEECLAW_UI_URL || "http://127.0.0.1:18789";
const artifactDir = process.env.LEECLAW_UI_ARTIFACT_DIR || "/tmp";
await fs.mkdir(artifactDir, { recursive: true });
const context = await chromium.launchPersistentContext(`${artifactDir}/knowledge-browser-profile`, {
  executablePath: process.env.CHROMIUM_PATH || "/usr/bin/chromium",
  headless: true,
  args: ["--no-sandbox", "--disable-dev-shm-usage"],
  viewport: { width: 1600, height: 1000 },
});

try {
  const page = context.pages()[0] || await context.newPage();
  const pageErrors = [];
  const failedKnowledgeRequests = [];
  page.on("pageerror", (error) => pageErrors.push(String(error)));
  page.on("response", (response) => {
    const pathname = new URL(response.url()).pathname;
    const emptyDocumentKBGraph = response.status() === 404 && pathname.endsWith("/wiki/graph");
    if (response.url().includes(":18791/") && response.status() >= 400 && !emptyDocumentKBGraph) {
      failedKnowledgeRequests.push(`${response.status()} ${pathname}`);
    }
  });

  await page.goto(baseURL, { waitUntil: "domcontentloaded", timeout: 30_000 });
  const startup = await Promise.race([
    page.locator("openclaw-login-gate").waitFor({ timeout: 30_000 }).then(() => "login"),
    page.locator("openclaw-app-shell").waitFor({ timeout: 30_000 }).then(() => "connected"),
  ]);
  if (startup === "login") {
    await page.getByLabel("Gateway secret", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Connect", exact: true }).click();
  }
  try {
    await page.locator("openclaw-app-shell").waitFor({ timeout: 30_000 });
  } catch (error) {
    await page.screenshot({ path: `${artifactDir}/knowledge-login-failure.png`, fullPage: true });
    const visible = (await page.locator("body").innerText()).replaceAll(password, "[redacted]");
    throw new Error(`OpenClaw native login did not complete: ${visible.slice(0, 2000)}`, { cause: error });
  }

  const knowledgeNavigation = page.getByText("Knowledge", { exact: true }).first();
  await knowledgeNavigation.waitFor({ timeout: 30_000 });
  await knowledgeNavigation.click();

  const iframe = page.locator("iframe.leeclaw-knowledge-frame");
  await iframe.waitFor({ state: "attached", timeout: 30_000 });
  const native = page.frameLocator("iframe.leeclaw-knowledge-frame");
  await native.locator("body").waitFor({ state: "visible", timeout: 30_000 });
  await native.locator("body").filter({ hasText: /Knowledge|知识库/i }).waitFor({ timeout: 30_000 });

  const frameURL = await iframe.getAttribute("src");
  const parsedFrameURL = frameURL ? new URL(frameURL) : null;
  if (!parsedFrameURL || parsedFrameURL.port !== "18791" || parsedFrameURL.pathname !== "/auth/session" ||
      !parsedFrameURL.searchParams.get("next")?.startsWith("/platform/knowledge-bases")) {
    throw new Error(`unexpected native Knowledge frame URL: ${frameURL}`);
  }

  const nativeBody = await native.locator("body").innerText();
  if (/登录|sign in|log in/i.test(nativeBody)) {
    throw new Error("WeKnora exposed a second login page instead of the workspace session");
  }
  const shellText = await page.locator("openclaw-app-shell").innerText();
  for (const forbidden of ["Memory", "Skills", "Workspaces"]) {
    const exactLines = shellText.split("\n").map((line) => line.trim()).filter(Boolean);
    if (exactLines.includes(forbidden)) throw new Error(`unexpected LeeClaw sidebar entry: ${forbidden}`);
  }

  const knowledgeAPICheck = await native.locator("body").evaluate(async () => {
    const response = await fetch("/api/v1/knowledge-bases", { credentials: "include" });
    const payload = response.ok ? await response.json() : null;
    const candidates = [payload?.data, payload?.data?.list, payload?.data?.items, payload?.items];
    const items = candidates.find((value) => Array.isArray(value)) || [];
    return { status: response.status, contentType: response.headers.get("content-type") || "", kbId: items[0]?.id || "" };
  });
  if (knowledgeAPICheck.status !== 200 || !knowledgeAPICheck.contentType.includes("json")) {
    throw new Error(`native Knowledge API failed: ${JSON.stringify(knowledgeAPICheck)}`);
  }
  if (!knowledgeAPICheck.kbId) throw new Error("native Knowledge API returned no knowledge base");
  const graphAPICheck = await native.locator("body").evaluate(async (kbId) => {
    const response = await fetch(`/api/v1/knowledgebase/${encodeURIComponent(kbId)}/wiki/graph`, { credentials: "include" });
    return { status: response.status, contentType: response.headers.get("content-type") || "" };
  }, knowledgeAPICheck.kbId);
  if (![200, 404].includes(graphAPICheck.status) || !graphAPICheck.contentType.includes("json")) {
    throw new Error(`native graph API failed: ${JSON.stringify(graphAPICheck)}`);
  }

  await page.screenshot({ path: `${artifactDir}/knowledge-native.png`, fullPage: true });
  if (pageErrors.length) throw new Error(`browser page errors: ${pageErrors.join(" | ")}`);
  const importantFailures = failedKnowledgeRequests.filter((item) => !item.startsWith("404 /favicon"));
  if (importantFailures.length) throw new Error(`Knowledge HTTP failures: ${importantFailures.join(" | ")}`);

  console.log("PASS [ui:login] OpenClaw native Login Gate authenticated the durable profile");
  console.log("PASS [ui:navigation] only the LeeClaw Knowledge extension is visible");
  console.log("PASS [ui:knowledge] WeKnora native Knowledge page and API loaded through :18791");
  console.log(`PASS [ui:graph] WeKnora native graph route is authenticated (HTTP ${graphAPICheck.status})`);
  console.log(`PASS [ui:artifact] ${artifactDir}/knowledge-native.png`);
} finally {
  await context.close();
}

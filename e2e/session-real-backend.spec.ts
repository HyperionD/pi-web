import { expect, test } from "@playwright/test";

type DurableEvent = {
  seq: string;
  type: string;
  payload?: Record<string, unknown>;
};

test("persists control events and replays them from the durable SSE cursor", async ({ page }) => {
  const baseURL = process.env.PI_WEB_E2E_BASE_URL || "http://127.0.0.1:4321";
  const password = "e2e-session-password";
  const setup = await page.request.post(`${baseURL}/api/admin/password`, {
    data: { password, confirmation: password },
  });
  if (![200, 201, 409].includes(setup.status())) throw new Error(`admin setup failed: ${setup.status()}`);
  const login = await page.request.post(`${baseURL}/api/account/login`, {
    data: { username: "admin", password },
  });
  if (!login.ok()) throw new Error(`login failed: ${login.status()}`);
  const setCookie = login.headers()["set-cookie"] || "";
  const cookie = /(?:^|,\s*)pi_web_session=([^;]+)/.exec(setCookie)?.[1];
  if (!cookie) throw new Error("login did not return a session cookie");
  await page.context().addCookies([{ name: "pi_web_session", value: cookie, url: baseURL }]);
  await page.goto("/api/health");

  const session = await page.evaluate(async () => {
    const health = await fetch("/api/health", { credentials: "include" });
    const healthBody = await health.json();
    if (!healthBody.durableEvents) throw new Error(`backend is not durable: ${JSON.stringify(healthBody)}`);
    const workspace = await fetch("/api/workspaces/open", {
      method: "POST",
      headers: { "content-type": "application/json" },
      credentials: "include",
      body: JSON.stringify({ path: "/tmp/pi-web-session-e2e/workspace" }),
    });
    if (!workspace.ok) throw new Error(`workspace open failed: ${workspace.status}`);
    const workspaceBody = await workspace.json();
    const created = await fetch(`/api/workspaces/${workspaceBody.id}/sessions`, {
      method: "POST",
      headers: { "content-type": "application/json", "x-client-request-id": "real-e2e-create" },
      credentials: "include",
    });
    if (!created.ok) throw new Error(`session create failed: ${created.status}`);
    const body = await created.json();
    return { workspaceId: workspaceBody.id, sessionId: body.session.id };
  });

  const firstBatch = await page.evaluate(async ({ sessionId }) => {
    const events: DurableEvent[] = [];
    const source = new EventSource(`/api/sessions/${sessionId}/events-v2?after=0`);
    const received = new Promise<DurableEvent[]>((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("timed out waiting for durable rename")), 5000);
      source.addEventListener("session.event", (event) => {
        const parsed = JSON.parse(event.data) as DurableEvent;
        events.push(parsed);
        if (parsed.type === "session.renamed") {
          clearTimeout(timeout);
          source.close();
          resolve(events);
        }
      });
      source.addEventListener("error", () => {
        clearTimeout(timeout);
        source.close();
        reject(new Error("durable SSE failed"));
      });
    });
    const rename = await fetch(`/api/sessions/${sessionId}`, {
      method: "PATCH",
      headers: { "content-type": "application/json", "x-client-request-id": "real-e2e-rename" },
      credentials: "include",
      body: JSON.stringify({ title: "real durable session" }),
    });
    if (rename.status !== 202) throw new Error(`rename failed: ${rename.status}`);
    return received;
  }, session);

  expect(firstBatch.map((event) => event.type)).toContain("session.renamed");
  const state = await page.evaluate(async (sessionId) => {
    const response = await fetch(`/api/sessions/${sessionId}/state`, { credentials: "include" });
    if (!response.ok) throw new Error(`state failed: ${response.status}`);
    return response.json();
  }, session.sessionId);
  expect(state.metadata.title).toBe("real durable session");

  const replay = await page.evaluate(async ({ sessionId, after }) => {
    const events: DurableEvent[] = [];
    const source = new EventSource(`/api/sessions/${sessionId}/events-v2?after=${after}`);
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(() => {
        source.close();
        reject(new Error("timed out waiting for durable replay"));
      }, 5000);
      source.addEventListener("session.event", (event) => {
        const parsed = JSON.parse(event.data) as DurableEvent;
        events.push(parsed);
        if (parsed.type === "session.renamed") {
          clearTimeout(timeout);
          source.close();
          resolve();
        }
      });
      source.addEventListener("error", () => {
        if (events.length) {
          clearTimeout(timeout);
          source.close();
          resolve();
          return;
        }
        clearTimeout(timeout);
        source.close();
        reject(new Error("durable replay SSE failed"));
      });
    });
    return events;
  }, { sessionId: session.sessionId, after: "0" });

  expect(replay.some((event) => event.type === "session.renamed")).toBe(true);
});

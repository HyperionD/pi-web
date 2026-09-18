// @ts-nocheck
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  getWorkspaceSkills,
  setWorkspaceSkillEnabled,
} from "../../lib/api";
import { cleanupPiAppFixture, installPiAppFixture } from "../test-helper";

describe("skill API client", () => {
  beforeEach(() => {
    installPiAppFixture();
    globalThis.PI_WEB_API_BASE = "http://backend.test";
  });

  afterEach(() => {
    cleanupPiAppFixture();
  });

  it("loads skills for the active workspace", async () => {
    globalThis.fetch = vi.fn(async (url) => {
      expect(String(url)).toContain("/api/workspaces/ws-1/skills");
      return new Response(JSON.stringify({ skills: [], available: true }), { status: 200 });
    });

    await getWorkspaceSkills("ws-1");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("patches a skill enabled state", async () => {
    globalThis.fetch = vi.fn(async (_url, options) => {
      expect(options.method).toBe("PATCH");
      expect(JSON.parse(options.body)).toEqual({ path: "/tmp/SKILL.md", enabled: false });
      return new Response(JSON.stringify({ skill: { path: "/tmp/SKILL.md", enabled: false } }), { status: 200 });
    });

    await setWorkspaceSkillEnabled("ws-1", "/tmp/SKILL.md", false);
    expect(fetch).toHaveBeenCalledTimes(1);
  });
});

describe("skill manager structure", () => {
  it("keeps the skill entry beside the account entry", async () => {
    const source = await import("../../App.astro?raw");
    expect(source.default).toContain("data-action=\"open-skill-manager\"");
    expect(source.default).toContain("Sparkles");
  });

  it("loads only the active workspace and restores focus on close", async () => {
    const app = installPiAppFixture();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "ws-1";
    const button = app.querySelector("[data-action='open-skill-manager']");
    button.focus();
    globalThis.fetch = vi.fn(async (url) => {
      expect(String(url)).toContain("/api/workspaces/ws-1/skills");
      return new Response(JSON.stringify({
        available: true,
        skills: [{
          name: "demo", description: "Demo", path: "/tmp/demo/SKILL.md", scope: "project", enabled: true,
          sourceKind: "local", sourceId: "project:.pi/skills", sourceLabel: "项目自定义技能",
        }],
      }), { status: 200 });
    });

    await app.openSkillManager();
    expect(app.querySelector("[data-skill-manager]").hidden).toBe(false);
    expect(app.querySelector("[data-skill-name]").textContent).toBe("demo");
    expect(app.querySelector("[data-skill-list] .skill-manager-item").textContent).toContain("demo");
    expect(app.querySelector("[data-skill-source-group]").textContent).toContain("项目自定义技能");
    app.closeSkillManager();
    expect(app.querySelector("[data-skill-manager]").hidden).toBe(true);
    expect(document.activeElement).toBe(button);
  });

  it("updates the selected skill and keeps the old state on failure", async () => {
    const app = installPiAppFixture();
    app.apiConnected = true;
    app.dataset.activeWorkspaceId = "ws-1";
    let shouldFail = false;
    globalThis.fetch = vi.fn(async (url, options) => {
      if (options?.method === "PATCH") {
        if (shouldFail) return new Response(JSON.stringify({ error: "denied" }), { status: 403 });
        return new Response(JSON.stringify({ skill: { name: "demo", description: "Demo", path: "/tmp/demo/SKILL.md", scope: "project", enabled: false } }), { status: 200 });
      }
      return new Response(JSON.stringify({
        available: true,
        skills: [{ name: "demo", description: "Demo", path: "/tmp/demo/SKILL.md", scope: "project", enabled: true }],
      }), { status: 200 });
    });

    await app.openSkillManager();
    const toggle = app.querySelector("[data-skill-toggle]");
    toggle.checked = false;
    await app.toggleSelectedSkill();
    expect(app.querySelector("[data-skill-state]").textContent).toContain("禁用");

    shouldFail = true;
    toggle.checked = true;
    await app.toggleSelectedSkill();
    expect(app.querySelector("[data-skill-state]").textContent).toContain("禁用");
    expect(app.querySelector("[data-skill-status]").textContent).toContain("denied");
  });
});

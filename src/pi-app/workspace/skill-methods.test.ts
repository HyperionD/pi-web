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
});

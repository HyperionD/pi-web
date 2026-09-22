// @ts-nocheck
import { beforeEach, describe, expect, it } from "vitest";
import "../index";

function renderApp() {
  document.body.innerHTML = `
    <pi-app data-active-workspace-id="w1">
      <div class="prompt-presets" data-prompt-presets hidden>
        <span data-prompt-presets-label></span>
        <div data-prompt-preset-list></div>
      </div>
      <div data-prompt-preset-manager>
        <select data-prompt-preset-skill><option value="">无 Skill</option></select>
      </div>
      <textarea class="prompt-textarea"></textarea>
      <button class="send-btn" aria-disabled="true">send</button>
      <button class="attach-btn">attach</button>
      <input data-file-input type="file" />
    </pi-app>
  `;
}

describe("prompt preset UI methods", () => {
  beforeEach(() => {
    renderApp();
  });

  it("renders available skills as preset select options", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();
    app.slashCommands = [
      { name: "skill:debug", command: "/skill:debug", source: "skill", description: "调试问题" },
      { name: "review", command: "/review", source: "prompt", description: "普通命令" },
    ];
    app.promptPresetState = { selectedId: "", draft: { label: "", skill: "debug", prompt: "", enabled: true, order: 1 } };

    app.renderPromptPresetSkillOptions();

    expect([...app.querySelectorAll("[data-prompt-preset-skill] option")].map((option) => option.value)).toEqual(["", "debug"]);
  });

  it("renders only enabled presets in the composer bar", async () => {
    const app = document.querySelector("pi-app");
    await customElements.whenDefined("pi-app");
    app.connectedCallback();
    app.promptPresets = [
      { id: "disabled", label: "隐藏", skill: "", prompt: "隐藏", enabled: false, order: 1 },
      { id: "review", label: "代码审查", skill: "review", prompt: "审查", enabled: true, order: 20 },
      { id: "debug", label: "问题处理", skill: "debug", prompt: "分析", enabled: true, order: 10 },
    ];

    app.renderPromptPresetButtons();

    const buttons = [...app.querySelectorAll("[data-action='prompt-preset']")];
    expect(buttons.map((button) => button.textContent)).toEqual(["问题处理", "代码审查"]);
    expect(buttons[0].dataset.promptPresetId).toBe("debug");
    expect(app.querySelector("[data-prompt-presets]").hidden).toBe(false);
  });
});

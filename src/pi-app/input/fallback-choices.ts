const CHOICE_FENCE = /```json\s*([\s\S]*?)```/gi;
const CHOICE_FENCE_MARKER = "```json";

export function parseFallbackChoices(text = "") {
  const choices = [];
  for (const match of text.matchAll(CHOICE_FENCE)) {
    try {
      const parsed = JSON.parse(match[1]);
      if (parsed?.type !== "piweb_choice" || !parsed.id || !parsed.question || !Array.isArray(parsed.options)) continue;
      choices.push({
        id: String(parsed.id).slice(0, 120),
        question: String(parsed.question).slice(0, 1000),
        allowCustom: parsed.allowCustom === true,
        options: parsed.options.slice(0, 8).filter((option) => option?.label && option?.value).map((option) => ({
          label: String(option.label).slice(0, 120),
          value: String(option.value).slice(0, 2000),
          description: option.description ? String(option.description).slice(0, 1000) : "",
        })),
      });
    } catch {}
  }
  return choices.filter((choice) => choice.options.length > 0);
}

export function parseFallbackInputs(text = "") {
  const inputs = [];
  for (const match of text.matchAll(CHOICE_FENCE)) {
    try {
      const parsed = JSON.parse(match[1]);
      if (parsed?.type !== "piweb_input" || !parsed.id || !parsed.question) continue;
      inputs.push({
        id: String(parsed.id).slice(0, 120),
        question: String(parsed.question).slice(0, 1000),
        placeholder: parsed.placeholder ? String(parsed.placeholder).slice(0, 200) : "",
        multiline: parsed.multiline !== false,
      });
    } catch {}
  }
  return inputs;
}

export function stripFallbackChoices(text = "") {
  return text.replace(CHOICE_FENCE, (block, json) => {
    try {
      const type = JSON.parse(json)?.type;
      return type === "piweb_choice" || type === "piweb_input" ? "" : block;
    } catch {
      return block;
    }
  }).trim();
}

export function fallbackChoicePrompt(id, value) {
  if (!id || !String(value).trim()) return "";
  return `选项回复：\nid: ${id}\nvalue: ${value}`;
}

export function fallbackInputPrompt(id, value) {
  if (!id || !String(value).trim()) return "";
  return `输入回复：\nid: ${id}\nvalue: ${String(value).trim()}`;
}

export function parseFallbackChoiceAnswer(text = "") {
  return parseFallbackAnswer(text, "选项回复：");
}

export function parseFallbackInputAnswer(text = "") {
  return parseFallbackAnswer(text, "输入回复：");
}

function parseFallbackAnswer(text, prefix) {
  const id = text.match(/^id:\s*(.+)$/m)?.[1]?.trim();
  if (!text.startsWith(prefix) || !id) return undefined;
  return id.slice(0, 120);
}

function splitPossibleChoiceFenceStart(text) {
  for (let length = Math.min(CHOICE_FENCE_MARKER.length - 1, text.length); length > 0; length -= 1) {
    const suffix = text.slice(-length);
    if (CHOICE_FENCE_MARKER.startsWith(suffix)) return [text.slice(0, -length), suffix];
  }
  return [text, ""];
}

export function streamVisibleChoiceText(text = "") {
  let visible = "";
  let pending = text;
  while (pending) {
    const fenceStart = pending.indexOf(CHOICE_FENCE_MARKER);
    if (fenceStart < 0) {
      const [safe, maybeFence] = splitPossibleChoiceFenceStart(pending);
      visible += safe;
      pending = maybeFence;
      break;
    }
    visible += pending.slice(0, fenceStart);
    const fenceEnd = pending.indexOf("```", fenceStart + CHOICE_FENCE_MARKER.length);
    if (fenceEnd < 0) {
      pending = pending.slice(fenceStart);
      break;
    }
    const block = pending.slice(fenceStart, fenceEnd + 3);
    if (!parseFallbackChoices(block).length && !parseFallbackInputs(block).length) visible += block;
    pending = pending.slice(fenceEnd + 3);
  }
  return { visible, pending };
}

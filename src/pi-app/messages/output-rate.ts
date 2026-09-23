const CJK_PATTERN = /[\u3000-\u30ff\u3400-\u9fff\uf900-\ufaff\u{20000}-\u{2fa1f}\uac00-\ud7af]/u;
const MIN_SAMPLE_MS = 500;

export type OutputTokenEstimate = { text: string; tokens: number };

export function estimateOutputTokens(text: string) {
  let cjk = 0;
  let rest = 0;
  for (const character of text) {
    if (CJK_PATTERN.test(character)) cjk++;
    else rest++;
  }
  return cjk + rest / 4;
}

function isHighSurrogate(codeUnit: number) {
  return codeUnit >= 0xd800 && codeUnit <= 0xdbff;
}

function isLowSurrogate(codeUnit: number) {
  return codeUnit >= 0xdc00 && codeUnit <= 0xdfff;
}

export function estimateIncrementalOutputTokens(previous: OutputTokenEstimate | undefined, text: string) {
  if (!previous || !text.startsWith(previous.text)) return estimateOutputTokens(text);
  let suffixStart = previous.text.length;
  let baseTokens = previous.tokens;
  if (
    suffixStart > 0
    && suffixStart < text.length
    && isHighSurrogate(previous.text.charCodeAt(suffixStart - 1))
    && isLowSurrogate(text.charCodeAt(suffixStart))
  ) {
    baseTokens -= 1 / 4;
    suffixStart--;
  }
  return baseTokens + estimateOutputTokens(text.slice(suffixStart));
}

export function calculateOutputRate(tokens: number, startedAt: number, now: number) {
  const elapsedMs = now - startedAt;
  if (tokens <= 0 || elapsedMs < MIN_SAMPLE_MS) return null;
  return tokens / (elapsedMs / 1000);
}

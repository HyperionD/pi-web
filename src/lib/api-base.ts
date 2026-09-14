const DEV_API_BASE = "http://127.0.0.1:8732";
const DEV_PORTS = new Set(["4321", "6006"]);

export function apiBase() {
  if (globalThis.PI_WEB_API_BASE !== undefined) return globalThis.PI_WEB_API_BASE;
  if (DEV_PORTS.has(globalThis.location?.port)) return DEV_API_BASE;
  return "";
}

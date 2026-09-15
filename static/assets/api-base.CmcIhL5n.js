const t="http://127.0.0.1:8732",o=new Set(["4321","6006"]);function a(){return globalThis.PI_WEB_API_BASE!==void 0?globalThis.PI_WEB_API_BASE:o.has(globalThis.location?.port)?t:""}export{a};

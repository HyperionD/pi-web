import { mkdtemp, mkdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, describe, it } from "node:test";
import assert from "node:assert/strict";
import { checkSessionSingleStream } from "./check-session-single-stream.mjs";

const fixtures = [];

afterEach(async () => {
  await Promise.all(fixtures.splice(0).map((path) => rm(path, { recursive: true, force: true })));
});

async function fixture(files) {
  const root = await mkdtemp(join(tmpdir(), "session-stream-check-"));
  fixtures.push(root);
  await mkdir(join(root, "internal/piweb"), { recursive: true });
  await mkdir(join(root, "src/pi-app"), { recursive: true });
  for (const [path, contents] of Object.entries(files)) {
    const target = join(root, path);
    await mkdir(join(target, ".."), { recursive: true });
    await writeFile(target, contents);
  }
  return root;
}

describe("check-session-single-stream", () => {
  it("reports every forbidden production pattern", async () => {
    const root = await fixture({
      "internal/piweb/bad.go": [
        "broker.Publish(sessionID, \"old\", payload)",
        "appendMessageAndPublish(store, broker, sessionID, msg)",
        "sessionEvents(w, r)",
        "s.mux.HandleFunc(\"GET /api/sessions/{sessionID}/events\", handler)",
      ].join("\n"),
      "src/pi-app/bad.ts": 'import "./legacy-session-event-adapter";\n',
    });

    const violations = await checkSessionSingleStream(root);

    assert.equal(violations.length, 5);
    assert.ok(violations.some((violation) => violation.includes("broker publish")));
    assert.ok(violations.some((violation) => violation.includes("legacy message append")));
    assert.ok(violations.some((violation) => violation.includes("legacy session subscription")));
    assert.ok(violations.some((violation) => violation.includes("legacy session events route")));
    assert.ok(violations.some((violation) => violation.includes("legacy session event adapter")));
  });

  it("accepts the durable stream patterns", async () => {
    const root = await fixture({
      "internal/piweb/good.go": "events.Append(ctx, sessionID, draft)\n",
      "src/pi-app/good.ts": 'open("/api/sessions/" + id + "/events-v2?after=0");\n',
    });

    assert.deepEqual(await checkSessionSingleStream(root), []);
  });

  it("allows legacy calls only in compat adapter, never Broker writes", async () => {
    const root = await fixture({
      "src/pi-app/sessions/legacy-session-event-adapter.ts": "sessionEvents(sessionId);\n",
      "src/pi-app/other.ts": "sessionEvents(sessionId);\n",
      "static/generated.js": "broker.Publish(sessionID, \"old\", payload);\n",
    });

    const violations = await checkSessionSingleStream(root, { compat: true });

    assert.equal(violations.length, 1);
    assert.ok(violations[0].includes("src/pi-app/other.ts"));
  });

  it("still rejects Broker writes inside the compat adapter", async () => {
    const root = await fixture({
      "src/pi-app/sessions/legacy-session-event-adapter.ts": "Broker.Publish(sessionID, \"old\", payload);\n",
    });

    const violations = await checkSessionSingleStream(root, { compat: true });

    assert.equal(violations.length, 1);
    assert.ok(violations[0].includes("broker publish"));
  });
});

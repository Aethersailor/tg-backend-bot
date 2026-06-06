import assert from "node:assert/strict";
import test from "node:test";

import { detectBackend, fetchBackendInfo, parseExtendedInfo } from "./index.js";

test("detects the lightweight SubConverter-Extended response", () => {
  const result = detectBackend(
    "SubConverter-Extended v1.1.21-f1f875a backend"
  );

  assert.equal(result.type, "SubConverter-Extended");
  assert.deepEqual(result.info, {
    version: "v1.1.21",
    build: "f1f875a",
    build_date: "",
  });
});

test("parses the current bilingual version page", () => {
  const info = parseExtendedInfo(`
    <title>SubConverter-Extended</title>
    <span class="info-label">
      <span data-lang="en">Version</span>
      <span data-lang="zh">版本</span>
    </span>
    <div class="info-value">v1.1.21</div>
    <span class="info-label">
      <span data-lang="en">Build</span>
      <span data-lang="zh">构建</span>
    </span>
    <div class="info-value"><a href="/commit/f1f875a">f1f875a</a></div>
    <span class="info-label">
      <span data-lang="en">Build Date</span>
      <span data-lang="zh">构建日期</span>
    </span>
    <div class="info-value">2026-06-06</div>
  `);

  assert.deepEqual(info, {
    version: "v1.1.21",
    build: "f1f875a",
    build_date: "2026-06-06",
  });
});

test("requests and parses the lightweight response", async (t) => {
  t.mock.method(globalThis, "fetch", async (_url, options) => {
    assert.equal(options.headers.Origin, "https://tg-backend-bot.invalid");
    assert.equal(options.headers["Sec-Fetch-Mode"], "cors");
    assert.equal(options.headers["Sec-Fetch-Dest"], "empty");
    return new Response("SubConverter-Extended dev-ec7afdf backend\n");
  });

  const result = await fetchBackendInfo("https://example.com/version");

  assert.equal(result.ok, true);
  assert.equal(result.type, "SubConverter-Extended");
  assert.equal(result.info.version, "dev");
  assert.equal(result.info.build, "ec7afdf");
});

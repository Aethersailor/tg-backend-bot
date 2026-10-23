import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import test from "node:test";
import worker, { detectBackend, parseExtendedInfo } from "./index.js";

function card(label, value) {
  return `<span class="info-label">${label}</span>\n<div class="info-value">${value}</div>`;
}

test("recognizes plain Extended and original subconverter versions", () => {
  assert.deepEqual(parseExtendedInfo("SubConverter-Extended v1.10.5-abcdef1 backend"), {
    version: "v1.10.5", build: "abcdef1", build_date: "",
  });
  assert.equal(detectBackend("subconverter v0.9.0-abcdef1 backend").type, "subconverter");
  assert.equal(parseExtendedInfo("unrelated page"), null);
});

test("extracts bilingual HTML cards, nested value markup and mixed case", () => {
  const html = "SubConverter-Extended" +
    card('<span data-lang="en">Version</span><span data-lang="zh">版本</span>', "v1.10.5") +
    card("Build", '<a href="/commit/abcdef1">abcdef1</a>') +
    card("Build Date", "2026-10-22").toUpperCase();
  const expected = { version: "v1.10.5", build: "abcdef1", build_date: "2026-10-22" };
  assert.deepEqual(parseExtendedInfo(html), expected);
  assert.deepEqual(parseExtendedInfo(html), expected);
});

test("ignores incomplete cards and preserves ordinary text containing script", () => {
  const html = "SubConverter-Extended" + card("Version", "description subscription") +
    '<span class="info-label">Build</span><div class="info-value">unfinished';
  assert.deepEqual(parseExtendedInfo(html), {
    version: "description subscription", build: "", build_date: "",
  });
});

test("processes adversarial responses at the body limit without stalling", () => {
  const result = spawnSync(process.execPath, ["--input-type=module", "-e", `
    import { parseExtendedInfo } from ${JSON.stringify(new URL("./index.js", import.meta.url).href)};
    const wrap = value => 'SubConverter-Extended<span class="info-label">Version</span><div class="info-value">' + value + '</div>';
    const payloads = [
      'SubConverter-Extended' + '<span class="info-label">a'.repeat(5500),
      'SubConverter-Extended<span class="info-label">' + '</span><div class="info-value">a'.repeat(4200),
      wrap('<script>' + '</script\\t'.repeat(14000)),
      wrap('<'.repeat(131000)),
    ];
    for (const payload of payloads) parseExtendedInfo(payload.slice(0, 128 * 1024));
  `], { timeout: 2000, encoding: "utf8" });
  assert.ifError(result.error);
  assert.equal(result.status, 0, result.stderr);
});

test("sends backend text without enabling Telegram HTML parsing", async (t) => {
  let payload;
  t.mock.method(globalThis, "fetch", async (url, options) => {
    if (String(url).startsWith("https://api.telegram.org/")) {
      payload = JSON.parse(options.body);
      return new Response("ok");
    }
    return new Response("SubConverter-Extended" + card("Version", "<script>alert(1)</script>"));
  });
  const pending = [];
  const response = await worker.fetch(new Request("https://worker.invalid", {
    method: "POST",
    body: JSON.stringify({ message: { chat: { id: 1 }, text: "/backend" } }),
  }), { BOT_TOKEN: "test-token", BACKEND_URLS: "backend.invalid" }, {
    waitUntil(promise) { pending.push(promise); },
  });
  await Promise.all(pending);
  assert.equal(response.status, 200);
  assert.match(payload.text, /版本: alert\(1\)/);
  assert.equal(Object.hasOwn(payload, "parse_mode"), false);
  assert.equal(Object.hasOwn(payload, "entities"), false);
  assert.equal(/[<>]/.test(payload.text), false);
});

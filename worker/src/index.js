const DEFAULT_BACKEND = "api.asailor.org";
const MAX_BACKENDS = 20;
const MAX_CONCURRENCY = 5;
const REQUEST_TIMEOUT_MS = 10_000;
const BACKEND_BODY_LIMIT = 128 * 1024;
const TELEGRAM_LIMIT = 3900;

const VERSION_PATTERN = /^subconverter\s+v[\d.]+-[\w]+ backend$/i;
const EXTENDED_MARKER = /SubConverter-Extended/i;
const INFO_CARD_PATTERN = /<span class="info-label">\s*(Version|Build|Build Date)\s*<\/span>\s*<div class="info-value">(.*?)<\/div>/gis;
const TAG_PATTERN = /<[^>]+>/g;
const SCHEME_PATTERN = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\//;

const BACKEND_HEADERS = {
  "User-Agent":
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
  Accept:
    "text/plain,text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
  Connection: "keep-alive",
};

export default {
  async fetch(request, env, ctx) {
    if (request.method === "GET") {
      return new Response("ok");
    }

    if (request.method !== "POST") {
      return new Response("Method Not Allowed", { status: 405 });
    }

    if (!env || !env.BOT_TOKEN) {
      return new Response("BOT_TOKEN not set", { status: 500 });
    }

    if (env.WEBHOOK_SECRET) {
      const token = request.headers.get("X-Telegram-Bot-Api-Secret-Token");
      if (token !== env.WEBHOOK_SECRET) {
        return new Response("Unauthorized", { status: 401 });
      }
    }

    let update;
    try {
      update = await request.json();
    } catch (err) {
      return new Response("Bad Request", { status: 400 });
    }

    const message = update?.message || update?.edited_message;
    const chatId = message?.chat?.id;
    const text = message?.text;
    if (!chatId || !text) {
      return new Response("ok");
    }

    if (!isBackendCommand(text)) {
      return new Response("ok");
    }

    const reply = await buildStatusMessage(env);
    ctx.waitUntil(sendMessage(env.BOT_TOKEN, chatId, reply));
    return new Response("ok");
  },
};

function isBackendCommand(text) {
  const trimmed = text.trim();
  return trimmed === "/backend" || trimmed === "/后端状态" || trimmed === "后端状态";
}

async function sendMessage(token, chatId, text) {
  const payload = {
    chat_id: chatId,
    text: trimMessage(text, TELEGRAM_LIMIT),
    disable_web_page_preview: true,
  };

  const resp = await fetch(`https://api.telegram.org/bot${token}/sendMessage`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });

  if (!resp.ok) {
    const body = await resp.text();
    console.log("sendMessage failed", resp.status, body);
  }
}

async function buildStatusMessage(env) {
  const { targets, truncated } = loadBackendTargets(env);
  if (!targets.length) {
    return "未配置后端地址，请设置 BACKEND_URLS 环境变量。";
  }

  const results = await mapWithLimit(targets, MAX_CONCURRENCY, (target, index) =>
    fetchBackendInfo(target.url)
  );

  let onlineCount = 0;
  const blocks = results.map((result, index) => {
    if (result.ok) {
      onlineCount += 1;
    }
    return formatBackendBlock(index + 1, targets[index].display, result);
  });

  const offlineCount = results.length - onlineCount;
  let title = `后端状态 (${results.length}) 在线 ${onlineCount} / 离线 ${offlineCount}`;
  if (truncated) {
    title += ` - 仅显示前 ${MAX_BACKENDS} 个`;
  }

  return `${title}\n\n${blocks.join("\n\n")}`;
}

async function mapWithLimit(items, limit, mapper) {
  const results = new Array(items.length);
  let index = 0;
  const workers = new Array(Math.min(limit, items.length))
    .fill(0)
    .map(async () => {
      while (true) {
        const current = index++;
        if (current >= items.length) {
          break;
        }
        results[current] = await mapper(items[current], current);
      }
    });

  await Promise.all(workers);
  return results;
}

async function fetchBackendInfo(targetUrl) {
  try {
    const { status, text } = await fetchWithTimeout(
      targetUrl,
      REQUEST_TIMEOUT_MS
    );

    if (status !== 200) {
      return { ok: false, status, error: `HTTP ${status}` };
    }

    const trimmed = text.trim();
    const { type, info } = detectBackend(trimmed);
    return { ok: true, status, type, info };
  } catch (err) {
    if (err?.name === "AbortError") {
      return { ok: false, error: "timeout" };
    }
    return { ok: false, error: "connection_error" };
  }
}

async function fetchWithTimeout(url, timeoutMs) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const resp = await fetch(url, {
      headers: BACKEND_HEADERS,
      signal: controller.signal,
    });
    let text = await resp.text();
    if (text.length > BACKEND_BODY_LIMIT) {
      text = text.slice(0, BACKEND_BODY_LIMIT);
    }
    return { status: resp.status, text };
  } finally {
    clearTimeout(timer);
  }
}

function detectBackend(text) {
  const extendedInfo = parseExtendedInfo(text);
  if (extendedInfo) {
    return { type: "SubConverter-Extended", info: extendedInfo };
  }

  if (VERSION_PATTERN.test(text) || text.toLowerCase().includes("subconverter")) {
    return { type: "subconverter", info: { version: text } };
  }

  return { type: "unknown", info: { snippet: compactSnippet(text, 200) } };
}

function parseExtendedInfo(text) {
  if (!EXTENDED_MARKER.test(text)) {
    return null;
  }

  const info = { version: "", build: "", build_date: "" };
  let match;
  while ((match = INFO_CARD_PATTERN.exec(text))) {
    const label = match[1].trim().toLowerCase();
    const value = stripHtml(match[2]);
    if (!value) {
      continue;
    }
    if (label === "version") {
      info.version = value;
    } else if (label === "build") {
      info.build = value;
    } else if (label === "build date") {
      info.build_date = value;
    }
  }

  if (!info.version && !info.build && !info.build_date) {
    return null;
  }
  return info;
}

function stripHtml(value) {
  // Normalize whitespace first.
  let cleaned = String(value).replace(/\s+/g, " ");

  // Aggressively remove any script-like substrings in a loop to avoid
  // incomplete multi-character sanitization issues.
  let previous;
  do {
    previous = cleaned;
    cleaned = cleaned
      .replace(/<script/gi, "")
      .replace(/<\/script/gi, "");
  } while (cleaned !== previous);

  // Remove simple tag-like patterns, then ensure no angle brackets remain.
  cleaned = cleaned.replace(TAG_PATTERN, "").replace(/[<>]/g, "");

  return cleaned.trim();
}

function compactSnippet(text, limit) {
  const compact = text.replace(/\s+/g, " ").trim();
  if (compact.length > limit) {
    return `${compact.slice(0, limit)}...`;
  }
  return compact;
}

function formatBackendBlock(index, display, result) {
  const lines = [`[${index}] ${display}`];

  if (!result.ok) {
    lines.push("类型: 未知");
    lines.push("状态: 离线");
    if (result.error) {
      lines.push(`错误: ${result.error}`);
    }
    return lines.join("\n");
  }

  lines.push(`类型: ${result.type}`);
  lines.push("状态: 在线");

  if (result.type === "SubConverter-Extended") {
    if (result.info.version) {
      lines.push(`版本: ${result.info.version}`);
    }
    if (result.info.build) {
      lines.push(`构建: ${result.info.build}`);
    }
    if (result.info.build_date) {
      lines.push(`构建日期: ${result.info.build_date}`);
    }
  } else if (result.type === "subconverter") {
    if (result.info.version) {
      lines.push(`版本: ${result.info.version}`);
    }
  } else if (result.info.snippet) {
    lines.push(`内容: ${result.info.snippet}`);
  }

  return lines.join("\n");
}

function parseBackendList(value) {
  if (!value) {
    return [];
  }
  return value
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function normalizeBackendTarget(raw) {
  const trimmed = raw.trim();
  if (!trimmed) {
    return null;
  }

  let input = trimmed;
  if (!SCHEME_PATTERN.test(input)) {
    input = `https://${input}`;
  }

  let parsed;
  try {
    parsed = new URL(input);
  } catch (err) {
    return { display: trimmed, url: "" };
  }

  let path = parsed.pathname || "";
  if (!path || path === "/") {
    path = "/version";
  } else if (path.replace(/\/$/, "") === "/version") {
    path = "/version";
  } else {
    path = path.replace(/\/$/, "") + "/version";
  }

  parsed.pathname = path;
  parsed.search = "";
  parsed.hash = "";

  return { display: trimmed, url: parsed.toString() };
}

function loadBackendTargets(env) {
  let raw =
    (env && (env.BACKEND_URLS || env.BACKEND_URL)) || DEFAULT_BACKEND;
  raw = `${raw}`.trim();

  const items = parseBackendList(raw);
  const truncated = items.length > MAX_BACKENDS;
  const limited = items.slice(0, MAX_BACKENDS);

  const targets = [];
  for (const item of limited) {
    const normalized = normalizeBackendTarget(item);
    if (normalized && normalized.url) {
      targets.push(normalized);
    }
  }

  return { targets, truncated };
}

function trimMessage(text, limit) {
  if (!text || text.length <= limit) {
    return text;
  }
  return `${text.slice(0, limit)}...`;
}

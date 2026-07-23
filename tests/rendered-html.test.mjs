import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { access } from "node:fs/promises";
import net from "node:net";
import { fileURLToPath } from "node:url";
import test from "node:test";

const templateRootUrl = new URL("../", import.meta.url);
const templateRoot = fileURLToPath(templateRootUrl);
const previewRoot = new URL("../app/_sites-preview/", import.meta.url);

async function reservePort() {
  const server = net.createServer();
  await new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const address = server.address();
  const port = typeof address === "object" && address ? address.port : 0;
  await new Promise((resolve) => server.close(resolve));
  return port;
}

async function startProductionServer() {
  const port = await reservePort();
  const child = spawn(process.execPath, [".next/standalone/server.js"], {
    cwd: templateRoot,
    env: {
      ...process.env,
      HOSTNAME: "127.0.0.1",
      PORT: String(port),
      NODE_ENV: "production",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  let logs = "";
  child.stdout.on("data", (chunk) => { logs += chunk.toString(); });
  child.stderr.on("data", (chunk) => { logs += chunk.toString(); });

  const url = `http://127.0.0.1:${port}/`;
  for (let attempt = 0; attempt < 80; attempt += 1) {
    if (child.exitCode !== null) {
      throw new Error(`Joe üretim sunucusu erken kapandı.\n${logs}`);
    }
    try {
      const response = await fetch(url);
      if (response.ok) return { child, response };
    } catch {
      // Sunucu henüz dinlemeye başlamadı.
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  child.kill();
  throw new Error(`Joe üretim sunucusu zamanında hazır olmadı.\n${logs}`);
}

test("server-renders the Turkish Joe workspace", async (t) => {
  const { child, response } = await startProductionServer();
  t.after(() => child.kill());

  assert.equal(response.status, 200);
  assert.match(response.headers.get("content-type") ?? "", /^text\/html\b/i);

  const html = await response.text();
  assert.match(html, /<html[^>]*lang="tr"/i);
  assert.match(html, /<title>Joe/);
  assert.match(html, /Vaka Masası/);
  assert.match(html, />OSINT</);
  assert.match(html, /Kuramsal Konsey/);
  assert.doesNotMatch(html, /Hızlı analiz|Derin konsey|Kolektif oturum/);
  assert.doesNotMatch(html, /codex-preview|react-loading-skeleton|Your site is taking shape/i);
});

test("removes the disposable starter preview", async () => {
  await assert.rejects(access(new URL("public/_sites-preview", templateRootUrl)));
  await assert.rejects(access(previewRoot));
});

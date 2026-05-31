import { chromium } from "playwright";
import fs from "fs";
import path from "path";

const catalogPath = path.resolve("../../internal/service/ai/affiliate_catalog.json");
const cookiesPath = process.argv[2] || "../../shopee-cookies.txt";

function parseCookieHeader(header) {
  return header
    .split(";")
    .map((p) => p.trim())
    .filter(Boolean)
    .map((part) => {
      const i = part.indexOf("=");
      if (i <= 0) return null;
      return {
        name: part.slice(0, i).trim(),
        value: part.slice(i + 1).trim(),
        domain: ".shopee.vn",
        path: "/",
      };
    })
    .filter(Boolean);
}

function toK(v) {
  const n = Number(v || 0);
  if (!n) return 0;
  // Shopee VN API (2024+): 46500000000 = 465k VND
  if (n >= 100000000) return Math.round((n + 50000000) / 100000000);
  // Legacy format: 39500000 = 395k
  if (n >= 100000) return Math.round((n + 50000) / 100000);
  if (n >= 1000) return Math.round((n + 500) / 1000);
  return n;
}

function fmtPrice(min, max) {
  const a = toK(min);
  const b = toK(max);
  if (!a && !b) return "";
  if (!b || a === b) return `${a}k`;
  return `${Math.min(a, b)}k-${Math.max(a, b)}k`;
}

function pickItem(json) {
  const data = json?.data;
  if (!data) return null;
  const item = data.item ?? data.item_info ?? data;
  if (!item?.name && !item?.title) return null;
  const name = item.name || item.title;
  const priceMin = item.price_min ?? item.price ?? 0;
  const priceMax = item.price_max ?? priceMin;
  return { name, priceMin, priceMax };
}

const cookieText = fs.readFileSync(path.resolve(cookiesPath), "utf8").trim();
const cookies = parseCookieHeader(cookieText.replace(/^cookie:\s*/i, ""));
const rows = JSON.parse(fs.readFileSync(catalogPath, "utf8"));

const browser = await chromium.launch({ headless: true });
const context = await browser.newContext({
  locale: "vi-VN",
  userAgent:
    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
  viewport: { width: 1366, height: 900 },
});
await context.addCookies(cookies);

let ok = 0;
for (const entry of rows) {
  const page = await context.newPage();
  let captured = null;
  page.on("response", async (resp) => {
    try {
      const u = resp.url();
      if (!u.includes("/api/v4/") || resp.status() !== 200) return;
      if (!u.includes("item/get") && !u.includes("pdp/get")) return;
      const ct = resp.headers()["content-type"] || "";
      if (!ct.includes("json")) return;
      const json = await resp.json();
      const picked = pickItem(json);
      if (picked && (picked.priceMin || picked.priceMax)) captured = picked;
    } catch {
      /* ignore */
    }
  });

  try {
    await page.goto(entry.affiliate_link, { waitUntil: "domcontentloaded", timeout: 90000 });
    await page.waitForTimeout(5000);
    if (!captured) {
      const h1 = await page.locator("h1").first().textContent().catch(() => "");
      console.log(JSON.stringify({ id: entry.id, error: "no_api_payload", title: h1?.trim() }));
    } else {
      entry.price_range = fmtPrice(captured.priceMin, captured.priceMax);
      console.log(
        JSON.stringify({
          id: entry.id,
          ok: true,
          price_range: entry.price_range,
          listing: captured.name,
        }),
      );
      ok++;
    }
  } catch (e) {
    console.log(JSON.stringify({ id: entry.id, error: String(e) }));
  } finally {
    await page.close();
  }
}

await browser.close();

if (ok > 0) {
  fs.writeFileSync(catalogPath, JSON.stringify(rows, null, 2) + "\n");
  console.log(`\nWrote ${ok}/${rows.length} updates to ${catalogPath}`);
} else {
  console.log("\nNo updates written.");
  process.exit(2);
}

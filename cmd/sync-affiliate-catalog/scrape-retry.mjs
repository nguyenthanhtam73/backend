import { chromium } from "playwright";
import fs from "fs";
import path from "path";

const catalogPath = path.resolve("../../internal/service/ai/affiliate_catalog.json");
const cookiesPath = process.argv[2] || "../../shopee-cookies.txt";
const onlyIds = new Set(
  (process.argv[3] || "cerave-foaming-cleanser,boj-relief-sun,hada-labo-gokujyun,to-hyaluronic-acid,lrp-cicaplast-b5")
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean),
);

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
  if (n >= 100000000) return Math.round((n + 50000000) / 100000000);
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
  if (!priceMin && !priceMax) return null;
  return { name, priceMin, priceMax };
}

function parseDomPrices(text) {
  const nums = [];
  const re = /(\d{1,3}(?:\.\d{3})+|\d{4,7})\s*(?:₫|đ|VND|vnđ)/gi;
  for (const m of text.matchAll(re)) {
    const raw = m[1].replace(/\./g, "");
    const n = Number(raw);
    if (n >= 10000 && n <= 5000000) nums.push(n);
  }
  if (nums.length === 0) return null;
  nums.sort((a, b) => a - b);
  const min = nums[0];
  const max = nums[nums.length - 1];
  return { priceMin: min * 100000, priceMax: max * 100000 };
}

const cookieText = fs.readFileSync(path.resolve(cookiesPath), "utf8").trim();
const cookies = parseCookieHeader(cookieText.replace(/^cookie:\s*/i, ""));
const rows = JSON.parse(fs.readFileSync(catalogPath, "utf8")).filter((r) => onlyIds.has(r.id));

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
    await page.goto(entry.affiliate_link, { waitUntil: "networkidle", timeout: 120000 });
    await page.waitForTimeout(3000);

    if (!captured) {
      // Try direct product URL if we landed on product page
      const url = page.url();
      if (url.includes("shopee.vn")) {
        await page.reload({ waitUntil: "networkidle", timeout: 120000 }).catch(() => {});
        await page.waitForTimeout(3000);
      }
    }

    if (!captured) {
      const bodyText = await page.locator("body").innerText().catch(() => "");
      const dom = parseDomPrices(bodyText);
      if (dom) captured = { name: "", ...dom, source: "dom" };
    }

    const h1 = await page.locator("h1").first().textContent().catch(() => "");

    if (!captured) {
      console.log(JSON.stringify({ id: entry.id, error: "no_price", title: h1?.trim(), url: page.url() }));
    } else {
      entry.price_range = fmtPrice(captured.priceMin, captured.priceMax);
      console.log(
        JSON.stringify({
          id: entry.id,
          ok: true,
          price_range: entry.price_range,
          source: captured.source || "api",
          title: h1?.trim() || captured.name,
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
  const all = JSON.parse(fs.readFileSync(catalogPath, "utf8"));
  const byId = Object.fromEntries(rows.map((r) => [r.id, r.price_range]));
  for (const row of all) {
    if (byId[row.id]) row.price_range = byId[row.id];
  }
  fs.writeFileSync(catalogPath, JSON.stringify(all, null, 2) + "\n");
  console.log(`\nUpdated ${ok}/${rows.length} items in catalog`);
} else {
  console.log("\nNo updates.");
  process.exit(2);
}

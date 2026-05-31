import { chromium } from "playwright";

const links = [
  ["cerave-foaming-cleanser", "https://s.shopee.vn/70HPIj8Km4"],
  ["cerave-hydrating-cleanser", "https://s.shopee.vn/2qRqPmwzpY"],
  ["anua-heartleaf-toner", "https://s.shopee.vn/6AiINzImYf"],
  ["biore-uv-aqua-rich", "https://s.shopee.vn/1BJcMMK4au"],
  ["boj-relief-sun", "https://s.shopee.vn/7pqWIkqv8j"],
  ["klairs-supple-toner", "https://s.shopee.vn/20sjM9NQkT"],
  ["some-by-mi-miracle-toner", "https://s.shopee.vn/9KfK5f8jH1"],
  ["hada-labo-gokujyun", "https://s.shopee.vn/9UykI17ujY"],
  ["to-niacinamide", "https://s.shopee.vn/5VSbWhy8zA"],
  ["to-hyaluronic-acid", "https://s.shopee.vn/809wVLfzSU"],
  ["cosrx-snail-96", "https://s.shopee.vn/110CAVZWKG"],
  ["melano-cc-premium", "https://s.shopee.vn/4Va4KzPJbs"],
  ["axis-y-dark-spot", "https://s.shopee.vn/qglyJ74BF"],
  ["skin1004-centella-ampoule", "https://s.shopee.vn/1Ld2dpmuQ4"],
  ["neutrogena-hydro-boost", "https://s.shopee.vn/6AiIKBTlMf"],
  ["lrp-cicaplast-b5", "https://s.shopee.vn/7VDg438uB9"],
  ["cosrx-acne-patch", "https://s.shopee.vn/BR5BEJ1xB"],
];

function toK(v) {
  const n = Number(v || 0);
  if (!n) return 0;
  return n >= 100000 ? Math.round(n / 100000) : n;
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
  const price = item.price ?? item.price_min ?? 0;
  const priceMin = item.price_min ?? price;
  const priceMax = item.price_max ?? price;
  return { name, priceMin, priceMax };
}

async function scrapeOne(browser, id, link) {
  const page = await browser.newPage({
    viewport: { width: 1366, height: 900 },
    userAgent:
      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
    locale: "vi-VN",
  });

  let captured = null;
  page.on("response", async (resp) => {
    try {
      const u = resp.url();
      if (!u.includes("/api/v4/")) return;
      if (!u.includes("item/get") && !u.includes("pdp/get_pc") && !u.includes("pdp/get")) return;
      const ct = resp.headers()["content-type"] || "";
      if (!ct.includes("json")) return;
      const json = await resp.json();
      const picked = pickItem(json);
      if (picked) captured = picked;
    } catch {
      /* ignore */
    }
  });

  try {
    await page.goto(link, { waitUntil: "networkidle", timeout: 60000 });
    await page.waitForTimeout(3000);

    if (!captured) {
      const og = await page.locator('meta[property="og:title"]').getAttribute("content").catch(() => null);
      const h1 = await page.locator("h1").first().textContent().catch(() => null);
      const title = (h1 || og || "").replace(/\s*\|\s*Shopee.*$/i, "").trim();
      return { id, link, title, price_range: "", error: "no_api_payload" };
    }

    return {
      id,
      link,
      title: captured.name,
      price_range: fmtPrice(captured.priceMin, captured.priceMax),
      price_min: captured.priceMin,
      price_max: captured.priceMax,
      url: page.url(),
    };
  } catch (e) {
    return { id, link, error: String(e) };
  } finally {
    await page.close();
  }
}

const browser = await chromium.launch({ headless: true });
for (const [id, link] of links) {
  const row = await scrapeOne(browser, id, link);
  console.log(JSON.stringify(row));
}
await browser.close();

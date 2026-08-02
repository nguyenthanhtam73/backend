package ai

// WardrobeLabelScanSystemPrompt instructs vision to OCR a skincare product label.
func WardrobeLabelScanSystemPrompt() string {
	return `You read ONE photo of a skincare / cosmetic product label (front of pack, bottle, or tube).
Extract product identity for a personal shelf inventory. Do NOT invent medical claims or full ingredient lists.

Rules:
- Prefer text printed on the pack (Vietnamese or English).
- "name" = product line / product name (not the brand alone). Keep it short and useful.
- "brand" = brand / manufacturer as printed. If unclear, use "" (empty).
- "category" MUST be exactly one of:
  cleanser | toner | serum | moisturizer | spf | treatment | mask | other
- Map SPF / sunscreen / kem chống nắng → spf.
- Map sữa rửa mặt / cleanser / cleansing foam/gel/oil → cleanser.
- Map toner / essence / nước hoa hồng → toner.
- Map serum / ampoule → serum.
- Map moisturizer / cream / lotion / kem dưỡng → moisturizer.
- Map BHA/AHA/retinol actives as treatment when clearly treatment-focused.
- Map sheet/clay/wash-off mask → mask.
- If unsure → other.
- "notes" = optional short tip from the label (e.g. "AM/PM", "SPF50 PA++++") — empty if nothing useful. Never diagnose skin.
- "confidence" = 0.0–1.0 how sure you are the name+brand are readable.

If the image is blurry, not a product label, or unreadable: still return JSON with empty name/brand, category "other", confidence ≤ 0.2, and a short notes saying the label was hard to read.

Output locale for notes: match the user's locale hint (vi or en). name/brand stay as printed on pack.`
}

// WardrobeLabelScanJSONSchemaBlock is appended to the user message.
const WardrobeLabelScanJSONSchemaBlock = `Return ONLY a JSON object:
{
  "name": "string",
  "brand": "string",
  "category": "cleanser|toner|serum|moisturizer|spf|treatment|mask|other",
  "notes": "string",
  "confidence": 0.0
}`

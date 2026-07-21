# SePay deploy checklist (sandbox → Railway)

Use this before pointing the SePay dashboard IPN at production.

## 1. Code on Railway

- [ ] Merge / push payment routes (`POST /api/v1/payment/sepay/checkout`, `…/webhook`)
- [ ] Confirm deploy finished (`railway` / GitHub Actions green)
- [ ] Health: `GET https://backend-production-bfaa.up.railway.app/api/v1/health` → `{"status":"ok"}`
- [ ] Webhook route exists (not 404):
  ```bash
  curl -s -o /dev/null -w "%{http_code}" -X POST \
    https://backend-production-bfaa.up.railway.app/api/v1/payment/sepay/webhook \
    -H "Content-Type: application/json" \
    -d '{}'
  ```
  Expect **401** (missing/invalid secret), **not 404**.

## 2. Database

- [ ] AutoMigrate created `payment_orders` (or run `migrations/006_payment_orders.up.sql`)
- [ ] Smoke: create a checkout while logged in → row appears in `payment_orders` with `status=pending`

## 3. Railway env vars

Set on the **backend** service (sandbox for now):

| Variable | Value |
|----------|--------|
| `DADIARY_SEPAY_ENV` | `sandbox` |
| `DADIARY_SEPAY_MERCHANT_ID` | `SP-TEST-NT956599` |
| `DADIARY_SEPAY_SECRET_KEY` | (sandbox secret) |
| `DADIARY_PUBLIC_WEB_URL` | `https://dadiary.vn` |
| `DADIARY_SEPAY_SUCCESS_URL` | `https://dadiary.vn/payment/success` (optional if public web set) |
| `DADIARY_SEPAY_ERROR_URL` | `https://dadiary.vn/payment/error` |
| `DADIARY_SEPAY_CANCEL_URL` | `https://dadiary.vn/payment/cancel` |

Restart the service after changing env.

## 4. SePay dashboard (sandbox)

- [ ] IPN URL:
  ```
  https://backend-production-bfaa.up.railway.app/api/v1/payment/sepay/webhook
  ```
- [ ] Auth type: **SECRET_KEY**
- [ ] Secret = same as `DADIARY_SEPAY_SECRET_KEY`
- [ ] Save + use “Test send” if available

## 5. Frontend (Vercel)

- [ ] `NEXT_PUBLIC_API_URL=https://backend-production-bfaa.up.railway.app`
- [ ] Deploy pricing + `/payment/success|error|cancel` pages
- [ ] Smoke: logged-in → Pricing → Nâng cấp → lands on SePay sandbox

## 6. End-to-end smoke

- [ ] Pay with sandbox method
- [ ] Browser returns to `/payment/success` (EN: `/en/payment/success`)
- [ ] Success page shows **activating** then **active** (plan_tier paid)
- [ ] `GET /me` shows `premium` or `premium_plus`
- [ ] `payment_orders.status = paid` + `plan_change_logs` row with `sepay:…`

## 7. Before real money (later)

- [ ] Switch `DADIARY_SEPAY_ENV=production` + live merchant/secret
- [ ] Remove sandbox secret defaults from shared prod
- [ ] Plan expiry / renewals (monthly/yearly) — not shipped yet

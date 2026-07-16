// Package push sends Web Push notifications via VAPID (Phase 2).
//
// # Web Push limitations (read before adding “native app” features)
//
//   - Custom sound files: NOT supported. The OS / browser plays its default
//     notification sound (or none). Payload field `silent` can only suppress
//     sound/vibration — it cannot pick a custom ringtone.
//   - Badge: Android uses a small status-bar glyph. Best with a simple
//     monochrome asset; colour PNG icons work but look less crisp.
//   - Foreground tabs: browsers often suppress or de-prioritise system
//     notifications while the site is focused. The service worker posts
//     DADIARY_PUSH_FOREGROUND so the page can show an in-app toast instead.
//   - Delivery is best-effort; expired endpoints return HTTP 404/410 and we
//     delete the stored subscription.
package push

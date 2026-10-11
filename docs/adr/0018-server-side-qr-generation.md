# 0018: Server-Side QR Generation

## Status
Accepted (2026-10-11)

## Context

QR codes appeared in three places, both generated in the browser:

- The big screen's join QR (and the closing ceremony's recap QR) used the qrcodejs library from `cdnjs.cloudflare.com`. When that CDN failed to load in a browser, `QRCode` was undefined and the ReferenceError killed the entire inline script block — no QR, no D3 graph, no `EventSource`, and a "Loading..." phase label stuck forever. One dead CDN request took down the whole screen (host report, 2026-10-11).
- The personal pages' profile, meet and recap QRs were rendered through the third-party `api.qrserver.com` image API. Besides the same venue-WiFi fragility, this sent every participant's UUID-bearing URL to an external service — a privacy leak the app did not need to make.

ADR-0009 already decided that front-end assets are self-hosted and the app works offline at venues; QR generation contradicted the spirit of that decision.

## Decision

Generate QR codes **server-side**, as PNG, from a single endpoint:

- `GET /qr?data=...&size=...` → `image/png`, backed by `github.com/skip2/go-qrcode` (`renderQRCode(data, size)`). `size` defaults to 150 and is clamped to 32..1024; missing `data` is a 400; unencodable content is a 500.
- Relative `data` (starting with `/`) is absolutized with the request's scheme (`X-Forwarded-Proto` aware, `r.TLS` fallback) and host (`qrPayload`), so a QR always encodes a scannable absolute URL no matter which host the browser used.
- Templates reference the endpoint directly as an `<img src>`: the big screen's join QR (`/qr?data=/user`), the recap QR swap at closing (an img src change), and the profile/meet/recap QRs on the personal pages and fragments.
- The qrcodejs `<script>` tag, both `new QRCode(...)` blocks, and all `api.qrserver.com` URLs and their JS are removed. No QR-related JavaScript remains.

## Consequences

### Positive

1. The big screen no longer depends on any QR library loading — the graph, stream and phase label cannot be taken down by a QR failure.
2. QR codes work on venue WiFi with no third-party request at all.
3. Privacy: participant URLs never leave the server to be rendered as an image.
4. One dependency (`skip2/go-qrcode`, pure Go, no transitive deps) replaces a browser library and an external API.
5. QRs are cacheable (`Cache-Control: public, max-age=3600`) and identical for every viewer of the same URL.

### Negative

1. A new (small) dependency in `go.mod`.
2. The QR payload's host is the request's `Host` header — correct for direct access and reverse proxies that pass `Host`/`X-Forwarded-Proto`; a proxy that rewrites `Host` without `X-Forwarded-Host` would encode the wrong absolute URL (relative QRs would have been scanned against the wrong origin anyway — the browser-side `window.location.origin` was the only fully correct source, and it is gone by design).
3. ADR-0009's full asset self-hosting (Tailwind, htmx, marked, d3) remains unimplemented; this ADR fixes the QR slice only, where the failure was observed and where the privacy leak was real.

## Alternatives Considered

- **Vendor qrcodejs into `static/` and serve it locally** — keeps browser-side generation, but adds a file server and an embedded asset for what a 200-byte PNG endpoint does better; the big screen's script would still die on any future JS error before the graph wiring.
- **Keep `api.qrserver.com` for the personal pages** — works, but keeps the venue-WiFi fragility and the privacy leak (participant UUIDs to a third party). Rejected.
- **A different Go QR library** (`yeqown/go-qrcode`, `pquerna/otp`) — heavier (otp pulls in TOTP/HOTP machinery); `skip2/go-qrcode` is the smallest pure-Go option with a stable API.

## Related

- ADR-0009 (self-hosted front-end assets): this ADR brings QR generation in line with its offline-venue goal.
- ADR-0008 (quality assurance): `renderQRCode` and `qrPayload` are pure functions with unit tests; the handler and the template wiring are covered by page tests; the coverage gate covers `qr.go`.
- Issue #126

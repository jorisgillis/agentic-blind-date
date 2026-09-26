# Vendored front-end assets

Self-hosted per ADR-0009: every front-end asset is committed here at a pinned
version, checked against `CHECKSUMS.sha256` by `scripts/check-static-assets.sh`.
To upgrade a pinned file: download the new version, replace the file (keep the
version in its name), recompute its checksum, and update `CHECKSUMS.sha256`.

| File | Version | Source |
|---|---|---|
| `tailwind-browser-4.3.3.js` | 4.3.3 | https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4.3.3 |
| `htmx-1.9.12.min.js` | 1.9.12 | https://cdn.jsdelivr.net/npm/htmx.org@1.9.12/dist/htmx.min.js |
| `marked-12.0.2.min.js` | 12.0.2 | https://cdn.jsdelivr.net/npm/marked@12.0.2/marked.min.js |
| `d3-7.9.0.min.js` | 7.9.0 | https://cdn.jsdelivr.net/npm/d3@7.9.0/dist/d3.min.js |
| `qrcode-1.0.0.min.js` | 1.0.0 | https://cdn.jsdelivr.net/npm/qrcodejs@1.0.0/qrcode.min.js |

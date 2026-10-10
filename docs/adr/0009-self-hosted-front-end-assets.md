# 0009: Self-Hosted Front-End Assets and In-Browser Tailwind v4

## Status
Accepted (2026-09-24)

## Context

Every page loaded its front-end code from third-party CDNs: the Tailwind v3 Play CDN, htmx, marked and d3. Themes need Tailwind utilities that read CSS variables switched by `<html data-theme>`, and they may bring their own web fonts. The app runs at meetup venues whose network can be slow or offline, on Participants' phones.

Tailwind documents both of its browser ("Play CDN") builds as development-only. The production route it recommends is a build step, such as the standalone Tailwind CLI, which needs no Node.

## Decision

1. **Every front-end asset is self-hosted** in a `static/` folder that the app serves under `/static/`: the Tailwind build, htmx, marked, d3 and the Themes' web fonts. Pages make no third-party requests. The Docker image copies `static/` next to `templates/`.
2. **Tailwind v4's browser build** (`@tailwindcss/browser@4`) is used deliberately instead of a build step. It is vendored as a pinned file. Pages configure the Theme tokens in a `<style type="text/tailwindcss">` block with `@theme inline`, generated from the Theme catalogue, so utilities compile to `var(...)` references that follow `data-theme`.
3. **Vendored files are committed at pinned versions.** CI checks each file's version and checksum, so an asset can't change unnoticed.
4. **No unstyled flash:** an inline rule hides the page until Tailwind has compiled the styles, with a safety timeout of about one second so the page still shows if the script fails.

## Consequences

### Good
- The app works fully on a venue network without internet.
- No build tooling: `go run .` and `docker build` need nothing beyond Go, and developers never run a watcher.
- Themes stay a runtime switch of CSS variables, with no per-Theme build output.
- Pinned, checksummed assets make front-end changes explicit in review.

### Bad
- We ship a build that Tailwind calls development-only. Styles are compiled on each device after load, which costs CPU on slow phones. Hiding the page until styles are ready makes that time a brief blank moment instead of a flash of raw HTML.
- Upgrades are manual: download the new pinned file, then update its checksum.
- Every page carries the Theme token block. It is generated from one catalogue, so it can't drift.

## Alternatives Considered

### Standalone Tailwind CLI (build step)
Generates one static CSS file (with `@theme inline`), served by the app and committed, with CI checking it is fresh.
- **Pros:** Tailwind's documented production route; no in-browser compiling; no flash.
- **Cons:** a build tool in development and CI; generated output in diffs.
- **Decision:** rejected in favour of no build tooling. **Revisit if in-browser compiling proves too slow on Participants' phones:** the Theme tokens and `data-theme` switching carry over unchanged.

### Tailwind v4 (or v3) Play CDN from jsDelivr
- **Pros:** zero setup.
- **Cons:** development-only, a third-party request on every page, and it breaks offline.
- **Decision:** rejected, because the app must work on venue networks without internet.

## Related
- Wayfinder map "New UI and Themes", ticket "Do Themes ship with a Tailwind build step instead of the Play CDN" (`.scratch/ui-and-themes/issues/07-tailwind-build-step.md`)
- Research: branch `research/theme-delivery`
- GitHub: "Spec: Themes" (#36)

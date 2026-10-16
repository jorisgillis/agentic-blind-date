# Theming Specification

Per-participant themes with persona business cards, a host-selected Public
theme, and a fully theme-aware landing page walkthrough.

**Status: locked.** Every decision below is final and implementation-ready.
This document assembles the resolutions of the Wayfinder: Theming map
([#158](https://github.com/jorisgillis/agentic-blind-date/issues/158)); each
section cites the issue that holds its full resolution.

The model in one paragraph: a **Theme** is a named set of ten color roles
whose hex values are injected as CSS variables on `:root` and referenced
through Tailwind arbitrary values. Each Participant picks a theme in
Settings; the Host picks a **Public theme** for the public pages (the Big
Screen, the landing page, the Public Event Recap); everything else falls
back to Midnight, the current dark look, which renders pixel-identical to
today. A persona's color and emoji are **identity, not decoration**: they
survive theming on the "business card" and other identity surfaces, and
never convert to theme variables.

Domain terms — Theme, Public theme, Business card — are in `CONTEXT.md`.

## Contents

1. [The Theme type and registry](#1-the-theme-type-and-registry)
2. [CSS variables and injection](#2-css-variables-and-injection)
3. [Participant theme storage](#3-participant-theme-storage)
4. [The business card](#4-the-business-card)
5. [The Public theme](#5-the-public-theme)
6. [Page-by-page theme policy](#6-page-by-page-theme-policy)
7. [The conversion pattern](#7-the-conversion-pattern)
8. [The landing page walkthrough](#8-the-landing-page-walkthrough)
9. [The theme picker](#9-the-theme-picker)
10. [Rollout](#10-rollout)

---

## 1. The Theme type and registry

Source: [#159](https://github.com/jorisgillis/agentic-blind-date/issues/159),
values from [#165](https://github.com/jorisgillis/agentic-blind-date/issues/165).
New file: `theme.go` (package main).

```go
// Theme is a page-level palette: ten color roles injected as CSS variables.
// A Participant picks one in Settings; the Host picks the Public theme for
// the public pages. Role values are hex colors.
type Theme struct {
	Name         string // registry key, lowercase: "midnight", "daylight"
	Label        string // display name for the pickers: "Midnight", "Daylight"
	Background   string // page background          --color-bg
	Surface      string // cards, panels            --color-surface
	SurfaceAlt   string // inputs, hover surfaces   --color-surface-alt
	Text         string // primary text             --color-text
	TextMuted    string // secondary text           --color-text-muted
	Border       string // borders, dividers        --color-border
	Accent       string // buttons, links           --color-accent
	AccentText   string // text on accent           --color-accent-text
	AccentMuted  string // accent-tinted text       --color-accent-muted
	SurfaceHover string // hover bg for surfaces    --color-surface-hover
}

// themeDefault names the Theme used when a Participant has made no choice,
// and for the host console.
const themeDefault = "midnight"

// themes is the one source of Themes, ordered for the pickers.
var themes = []Theme{
	{Name: "midnight", Label: "Midnight", Background: "#030712", Surface: "#111827", SurfaceAlt: "#1f2937",
		Text: "#ffffff", TextMuted: "#9ca3af", Border: "#1f2937", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#818cf8", SurfaceHover: "#374151"},
	{Name: "daylight", Label: "Daylight", Background: "#f9fafb", Surface: "#ffffff", SurfaceAlt: "#f3f4f6",
		Text: "#111827", TextMuted: "#4b5563", Border: "#e5e7eb", Accent: "#4f46e5", AccentText: "#ffffff",
		AccentMuted: "#4f46e5", SurfaceHover: "#e5e7eb"},
}

// themeByName returns the named Theme; unknown names get the default.
func themeByName(name string) Theme {
	for _, t := range themes {
		if t.Name == name {
			return t
		}
	}
	for _, t := range themes {
		if t.Name == themeDefault {
			return t
		}
	}
	return themes[0]
}
```

Decisions locked:

- **Types**: plain `string` hex values (`#030712`), not Tailwind classes.
  The roles become CSS variables referenced via Tailwind arbitrary values
  (`bg-[var(--color-bg)]`); the registry never needs Tailwind class strings.
- **Registry**: a slice, not a map — the pickers iterate it in a stable
  order, mirroring the `personaPalette` slice idiom in `persona.go`. Names
  are the keys; `themeByName` does the lookup.
- **Default**: `themeDefault = "midnight"`, a const next to the registry.
  Lookup falls back through the registry rather than a synthetic theme, so
  the default is a real entry.
- **Validation**: `themeByName` absorbing unknown names *is* the validation
  the storage section needs — handlers pass the stored/submitted string
  straight through; invalid values render the default rather than erroring.
- **Relationship to `paletteColor`**: none, deliberately. `paletteColor` is
  persona identity on the business card — fixed palette, Tailwind classes;
  `Theme` is page-level roles — registry, hex values. They share no
  structure and no file.
- **Location**: new `theme.go`, package main — the codebase's flat
  one-module-per-file layout (`persona.go`, `eventstate.go`).
- **The ten roles**: the eight originals (Background, Surface, SurfaceAlt,
  Text, TextMuted, Border, Accent, AccentText) plus two added by the values
  ticket — `AccentMuted` (the link/subtle-highlight color, today
  `text-indigo-400`; one accent cannot serve both jobs: indigo-600 on the
  Midnight background is only 3.20:1 and indigo-400 on Daylight white is
  2.98:1) and `SurfaceHover` (hover direction flips between themes —
  lighten in Midnight, darken in Daylight — so no single derived value
  works). No `accentHover` role: the accent is identical in both themes, so
  `hover:bg-indigo-500` becomes `hover:brightness-110` on
  `bg-[var(--color-accent)]` — theme-agnostic, and white text on the
  lightened accent is still 5.42:1.

### The values, with their provenance

Every Midnight value is the current hardcoded Tailwind palette hex (v3
defaults), so the default theme renders pixel-identical to today. Daylight
is the same Tailwind ramp one step into the light: gray-50 page, white
cards, gray-100 inputs, gray-900 text on gray-200 borders. The accent stays
indigo-600 `#4f46e5` in both themes — the code already uses
`bg-indigo-600 hover:bg-indigo-500` in light contexts exactly as in dark
ones, and indigo is the brand across the landing page, the Big Screen
graph, and the walkthrough.

Three deliberate deviations from the ticket's tentative table, following
the code:

- **Border (Midnight) is gray-800 `#1f2937`, not gray-700**: the dominant
  card idiom in the code is `bg-gray-900 border border-gray-800`; nested
  `border-gray-700` items collapse into the single border role anyway.
- **TextMuted (Daylight) is gray-600 `#4b5563`, not gray-500**: gray-500
  has no headroom on the gray-50 background and fails at 4.39:1 on gray-100
  SurfaceAlt, where muted text demonstrably lands (placeholders, labels).
  Gray-600 clears every Daylight surface at ≥6.87:1.
- **TextMuted (Midnight) is gray-400, deliberately**: today's muted text is
  a gray-400/gray-500 mix, but gray-500 already fails AA on the dark
  background (4.16:1). Locking gray-400 (7.93:1) quietly fixes the failing
  cases as the conversion proceeds.

### Contrast validation (WCAG 2.1, relative luminance)

Required pairs, both themes — every one clears the 4.5:1 normal-text AA bar
(the 3:1 large-text bar is not needed anywhere):

| Pair | Midnight | Daylight |
|------|----------|----------|
| Text on Background | 20.13:1 | 16.98:1 |
| Text on Surface | 17.74:1 | 17.74:1 |
| TextMuted on Background | 7.93:1 | 7.23:1 |
| AccentText on Accent | 6.29:1 | 6.29:1 |

Supporting placements the templates actually contain, also all AA:

| Pair | Midnight | Daylight |
|------|----------|----------|
| Text on SurfaceAlt | 14.68:1 | 16.12:1 |
| TextMuted on Surface / SurfaceAlt | 6.99:1 / 5.78:1 | 7.56:1 / 6.87:1 |
| AccentMuted on Background / Surface / SurfaceAlt | 6.75:1 / 5.95:1 / 4.92:1 | 6.02:1 / 6.29:1 / 5.71:1 |
| Text on SurfaceHover | 10.31:1 | 14.33:1 |
| AccentText on accent + `brightness-110` hover | 5.42:1 | 5.42:1 |

Borders are decorative in the current design, so WCAG 1.4.11 is not applied
to them; the role keeps the visual look, not a contrast target.

---

## 2. CSS variables and injection

Source: [#160](https://github.com/jorisgillis/agentic-blind-date/issues/160).

### Naming convention

Ten variables on `:root`, kebab-case, prefixed `--color-`, one per role:

| Role | CSS variable | Tailwind reference |
|------|-------------|--------------------|
| `Background` | `--color-bg` | `bg-[var(--color-bg)]` |
| `Surface` | `--color-surface` | `bg-[var(--color-surface)]` |
| `SurfaceAlt` | `--color-surface-alt` | `bg-[var(--color-surface-alt)]` |
| `Text` | `--color-text` | `text-[var(--color-text)]` |
| `TextMuted` | `--color-text-muted` | `text-[var(--color-text-muted)]` |
| `Border` | `--color-border` | `border-[var(--color-border)]` |
| `Accent` | `--color-accent` | `bg-[var(--color-accent)]` |
| `AccentText` | `--color-accent-text` | `text-[var(--color-accent-text)]` |
| `AccentMuted` | `--color-accent-muted` | `text-[var(--color-accent-muted)]` |
| `SurfaceHover` | `--color-surface-hover` | `hover:bg-[var(--color-surface-hover)]` |

### Injection point: one template function, one line per page `<head>`

The mechanism is a **template function**, not a render-pipeline change. In
`NewHandler`'s `FuncMap` (next to the existing `colorName`/`textColor`
palette helpers):

```go
// themeCSS returns the inline <style> block that injects the named Theme's
// color roles as CSS variables on :root. An empty name gives the default.
func themeCSS(name string) template.HTML {
	t := themeByName(name)
	return template.HTML("<style>:root{" +
		"--color-bg:" + t.Background +
		";--color-surface:" + t.Surface +
		";--color-surface-alt:" + t.SurfaceAlt +
		";--color-text:" + t.Text +
		";--color-text-muted:" + t.TextMuted +
		";--color-border:" + t.Border +
		";--color-accent:" + t.Accent +
		";--color-accent-text:" + t.AccentText +
		";--color-accent-muted:" + t.AccentMuted +
		";--color-surface-hover:" + t.SurfaceHover +
		"}</style>")
}
```

Every full-page template (each is a standalone document with its own
`<head>`) gets exactly one line:

```html
<head>
  ...
  {{themeCSS .ThemeName}}
</head>
```

The `themeCSS` function also emits the walkthrough variables (section 8)
when rendering the landing page. `themeByName` absorbing unknown names
means an empty or bad name can never produce a broken `<style>` block.

Why a template function and not a render wrapper: `h.render()` keeps its
signature. The render data maps are ad-hoc per handler, and the theme name
arrives by different routes (participant record, event setting, none at
all) — pushing it through the pipeline would force every handler to pass an
extra argument alongside data it mostly already carries. One function, one
head line per page, opt-in per template: minimal diff, and pages that
haven't converted yet are simply unaffected.

### HTMX fragments: inherit, never inject

Fragments are returned as standalone HTTP responses but are only ever
**swapped into the page's DOM by HTMX** — after the swap they sit inside
the page's `<html>`, so the `:root` variables cascade to them. Fragments
never call `themeCSS` and never need their own injection.

Standalone fragment responses fetched without the surrounding page (curl,
a stray direct URL) render without variables — the Tailwind arbitrary
values resolve to nothing. That is not a user path, and no fallback is
added for it.

### No `var()` fallbacks in templates

The page-level `<style>` block is the single source of variable values.
Templates reference `bg-[var(--color-bg)]` with **no** fallback hex inside
`var()` — fallbacks would duplicate every hex from `theme.go` into the
templates and drift. A missing variable means a missing page injection,
which the one-line-per-page rule prevents.

---

## 3. Participant theme storage

Source: [#161](https://github.com/jorisgillis/agentic-blind-date/issues/161).

### Storage: a `theme` column

```sql
theme TEXT NOT NULL DEFAULT 'midnight'
```

On the `participants` table, in **both** places a column exists today,
matching the `meet_code`/`persona_*` pattern in `db.go`:

- the `CREATE TABLE IF NOT EXISTS participants` DDL in `NewDB`, after
  `meet_code`;
- the migrations slice in `NewDB`, whose errors are ignored because the
  column may already exist:

```go
`ALTER TABLE participants ADD COLUMN theme TEXT NOT NULL DEFAULT 'midnight'`,
```

The column stores the theme **name** (`"midnight"`, `"daylight"`), never
hex values or CSS — the registry in `theme.go` is the only place names
become colors. The literal `'midnight'` in the DDL deliberately duplicates
`themeDefault` from `theme.go`: SQL cannot reference a Go const, and the
two must stay in agreement (the DDL's value backfills existing rows and
covers every insert that omits the column). No backfill statement and no
index: the `ALTER TABLE … NOT NULL DEFAULT` backfills every existing row,
and no query ever looks participants up by theme.

### Participant struct and read plumbing

- `Theme string` on the `Participant` struct in `db.go`, beside `MeetCode`
  in column order.
- `theme` appended to the `selectParticipant` const — **no `COALESCE`
  needed**: unlike `meet_code` the column is `NOT NULL` with a default from
  birth, so it scans directly like `has_github`.
- `&p.Theme` appended to `scanParticipant`'s `Scan` args, same position.

One column, one select, one scan: every read path picks the theme up for
free — `store.Get(id)`, by-handle, by-meet-code, and the list queries all
go through `selectParticipant`.

### Read path into the render data: nothing moves

The personal-page handlers already fetch the Participant and place it in
the render data; `Theme` rides along in it. Each page template's `<head>`
calls `{{themeCSS …}}` with the name under the page's **existing** data key:

| Page | Data key | Head line |
|------|----------|-----------|
| `wait.html` | `Participant` (the wait map's base, `waitStatusData`) | `{{themeCSS .Participant.Theme}}` |
| `match.html` | `Me` (`cards.go` `matchPageData`) | `{{themeCSS .Me.Theme}}` |
| `onboard.html` | embedded `*Participant` (`onboardPage`) | `{{themeCSS .Theme}}` |

No handler signature change, no new fetch. Unauthenticated pages keep
`{{themeCSS ""}}` — default, no storage (section 6).

### Write path: `POST /user/theme/{id}`

One new route in `buildMux` (`main.go`), next to the settings routes:

```go
mux.HandleFunc("POST /user/theme/{id}", h.SaveTheme)
```

`SaveTheme` in `handlers.go`, following `SaveSettings`' shape —
`h.store.Get(r.PathValue("id"))`, 404 on unknown Participant,
`strings.TrimSpace` on the form value, 500 on a save failure:

```go
// POST /user/theme/{id} — saves the Participant's theme choice.
func (h *Handler) SaveTheme(w http.ResponseWriter, r *http.Request) {
	p, err := h.store.Get(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimSpace(r.FormValue("theme"))
	if err := h.store.Change(ParticipantChange{ID: p.ID, Theme: &name}); err != nil {
		http.Error(w, "saving your theme failed: "+err.Error(), 500)
		return
	}
	w.Header().Set("HX-Redirect", h.destination(p))
}
```

The form field is `theme`; the picker markup that posts it is section 9.
The save goes through `ParticipantStore.Change`, the store's typed write
seam. One new field on the change struct:

```go
// ParticipantChange: nil leaves the theme alone, like every other field.
Theme *string
```

and one branch in `Change`, mirroring the existing column writes:

```go
if c.Theme != nil {
	if err := execFound(tx, c.ID, `UPDATE participants SET theme = ? WHERE id = ?`, *c.Theme, c.ID); err != nil {
		return err
	}
}
```

`execFound` reports `ErrParticipantNotFound` for an unknown id; `inTx`
commits atomically and announces the change on the feed — the same
guarantees every other Participant column write has.

**The response is `HX-Redirect` to `h.destination(p)`, with no body** — the
`WaitStatus` idiom, not the `SaveSettings` fragment re-render. Fragments
inherit the page's `:root` variables and never inject: a
`fragment-settings.html` response cannot change the page's colors, only a
re-rendered `<head>` can. HTMX performs the full-page navigation to the
Participant's canonical page, which re-renders the head with the new theme
(and the picker's current-theme marking) in one stroke.

### Validation: applied at render, not gated at write

`themeByName` absorbing unknown names **is** the validation; handlers pass
the stored/submitted string straight through. The write path stores the
trimmed submitted name **as-is** — no rejection branch, no 400, no
normalization to the registry. "Invalid names fall back to the default" is
satisfied where the string is consumed: `themeCSS` → `themeByName` renders
Midnight for any unknown stored name, and the picker marks no unknown name
selected. A garbage value in the column is harmless by construction — the
lenient rule is deliberate (a rejected theme save would strand a
participant's settings form for no benefit), and it is also the defense for
any name stored before a theme is removed from the registry. `TrimSpace`
is the only write-path touch.

---

## 4. The business card

Source: [#162](https://github.com/jorisgillis/agentic-blind-date/issues/162).

One principle underneath every decision: **a persona's color and emoji are
identity, not decoration**. They render from `personaPalette` (`persona.go`)
— stored Tailwind classes and hexes — and never reference theme variables.
Page chrome (backgrounds, panels, text, borders, accents) renders from the
Theme's `--color-*` roles. The two palettes share no structure, no file,
and no rule.

### What the business card is

A persona's **identity block**: the persona color as its background, the
emoji on it, the persona name and tagline in classes that read on that
color. Three renderings exist in the code:

| Surface | Template | Whose color |
|---|---|---|
| The owner's persona card | `fragment-persona-card.html` (wait page) | `{{.Participant.PersonaColor}}` |
| The target on the meet page | `meet.html`'s identity block | `{{.Target.PersonaColor}}` |
| The owner's recap block | `recap.html`'s identity block | `{{.Me.PersonaColor}}` |

Today none of these blocks carries the persona color itself — the *page
body* does (`<body class="{{…PersonaColor}}">`), and the card is a
transparent column over it (the emoji tile is translucent `bg-white/20`).
So theming **moves** the color from the page body to the identity block:
the block of emoji + name + tagline becomes one persona-colored panel —
the stored class (e.g. `bg-teal-400`) goes on the panel, rounded like the
page's other cards; the tile's `bg-white/20` goes, because the block itself
is now the color. The wait card's remaining content — the "This is you"
line and the QR — sits **outside** the business card and follows the theme.

The placeholder state (persona still crafting) keeps the same treatment:
`persona_color` and `persona_symbol` are assigned at registration
(`CreateParticipant`, `db.go`), before the persona exists, so the
placeholder panel is persona-colored too (🎭, pulse animation unchanged).

### Text on persona colors

- `paletteText` (`persona.go`) **stays verbatim**, including the
  unknown-class fallback to `text-gray-900`; the `textColor` template
  function that exposes it **stays**. The persona name on the business card
  renders `{{textColor .PersonaColor}}` — readable on its persona color
  under every theme.
- Secondary text on a persona-colored surface (tagline, chip captions)
  keeps its **hardcoded** `text-gray-9xx` classes. Text on a fixed palette
  background must not follow the theme — the theme's `--color-text` has no
  duty to read on `bg-teal-400`. Persona palette classes and their
  companion text classes never convert to variables.

### Page background change

`wait.html` and `match.html` become:

```html
<body class="bg-[var(--color-bg)] text-[var(--color-text)] min-h-screen px-4 py-…">
```

(keep each page's existing padding). What comes with it:

- `wait.html`'s body-level hardcoded `text-gray-900` is replaced by
  `text-[var(--color-text)]`.
- The full-page text wrappers
  `<div class="{{textColor .Me.PersonaColor}}">` (`match.html`, `meet.html`,
  `recap.html`) are **deleted** — the body class is the text color now.
- The `style="color-scheme: light"` on the `match`/`meet`/`recap` bodies
  goes with it: it exists because the persona-colored body was a light
  color. If a Theme ever needs a `color-scheme`, it is Theme data, not
  template hardcoding.
- **`recap.html` gets the same body change** — a third personal page with
  a persona-colored body. Same pattern, same rule; leaving it would keep a
  persona color as a page background on exactly one page.

### The meet page

`meet.html`'s body loses `{{.Target.PersonaColor}}` and gets the themed
body class above; its head gets the one `{{themeCSS .ThemeName}}` line.
**Whose theme: the viewer's** — `.Me`'s theme, `""` (default) on the
anonymous path where `.Me` is absent. The target's identity block keeps
`{{.Target.PersonaColor}}` per the business card rule: the viewer's theme
pages the room around it, the target's color identifies the target.

### Where persona colors appear regardless of theme (the inventory)

**Business cards** — the three identity blocks above.

**Persona chips** — small rounded squares: persona color background, emoji
centered, no text *on* the color; the adjacent name/status text follows the
theme.

- Participant pages (`fragments.html`): the matched teaser in
  `fragment-wait-status.html`, "Your meetings", `fragment-meet-result`,
  show & tell signup and presenter, board post attribution.
- `fragment-phase-content.html`: the match reveal and "Find them" chips.
- `fragment-mission-card.html`: the 3×3 grid tiles — filled tiles keep
  `.Color`; the empty-tile fallback `bg-black/5` is page chrome and
  converts per section 7.
- Host console (`fragments.html` on `admin.html` — default theme): show &
  tell queue, audience questions, board queue; `data.html` renders the
  stored class as a literal swatch.
- Public event recap (`event-recap.html`): match, persona gallery,
  presenter, board posters.

**The Big Screen** (`screen.html` + `screen.go`): persona hexes come from
`paletteHex()` → the `colorMap` in `screen.html`'s JS — graph node circles,
leaderboard name colors, the board rotation card's symbol background and
name, the presenter name. These keep persona hexes under whatever
host-selected theme the Big Screen gets; the grey fallback for unknown
classes stays.

**Copy that claims a page background**:
`fragment-phase-content.html` says "🦊 on a **teal background**" — written
for today's persona-colored page bodies. Once pages are themed, the
target's page shows their *theme*, not their palette color; the hint must
point at what still carries the color: their card and chips ("look for 🦊 —
their card is teal"). Exact wording is implementation; the lock is that the
copy stops claiming a page background. This copy change ships with theming
(see section 10), not after it.

### Contrast

- **The theme's duty**: `Text` and `TextMuted` readable on `Background` —
  satisfied by the values in section 1.
- **The palette's duty**: `paletteText` and its companion gray-9xx classes
  read on the light 400-level palette colors — already true today,
  unchanged by theming.
- **Persona colors as accents on a themed background** (chips, graph nodes)
  carry no body-text contrast duty: the emoji is the label, and names sit
  in theme text *next to* the color, never on it.

---

## 5. The Public theme

Source: [#163](https://github.com/jorisgillis/agentic-blind-date/issues/163),
decided in grilling with the user.

The Host-selected theme for the public pages — one theme, chosen by the
Host, for every public-facing page. Participants' personal pages stay
per-participant; the Host never controls them.

- **Concept and name: Public theme.** Storage key, console label, and spec
  language all say `public_theme` / "Public theme". The Big Screen is one
  public surface among three, not the namesake.
- **Surfaces**: `screen.html` (the Big Screen), `landing.html`, and
  `event-recap.html` (`/recap`). Each injects `{{themeCSS .PublicTheme}}`
  per section 2's mechanism; the handler resolves the name so templates
  stay one line.
- **Storage**: a `public_theme` key in the `event_state` key-value table,
  read and written by `EventStore` (`INSERT OR REPLACE`, the `phase`
  pattern). No new table, no schema change.
- **Console UI**: a "Public theme" section in the Host panel (`/admin`): a
  **swatch row** — one swatch per theme, built from the same registry, the
  current theme's swatch highlighted — mirroring the personal-page picker's
  idiom (section 9) so both pickers read as the same control.
- **Save flow**: `POST /admin/public-theme` — a plain form POST with a 303
  redirect to `/admin`, the existing admin idiom (`AdminBoard` et al.).
- **Default**: `midnight` — the Big Screen typically runs in a dim room. An
  absent `public_theme` key renders the default; `themeByName` also
  absorbs any bad stored value.
- **Graph edge colors stay fixed**: the D3 edges (green meeting, pink
  match, blue potential) and node hexes (persona colors, section 4) do not
  become theme-aware. The theme recolors the canvas
  (background/surface/text/border).
- **The landing page's walkthrough converts fully**: every hardcoded hex in
  `landing.html`'s inline style block becomes a CSS variable with
  per-theme values. Hexes whose Midnight value matches a theme role alias
  that role's variable; the rest get dedicated walkthrough variables
  (section 8).
- **The public event recap joins the Public theme** — same key, same
  injection line.

---

## 6. Page-by-page theme policy

Source: [#164](https://github.com/jorisgillis/agentic-blind-date/issues/164),
with the Public theme corrections folded in
([#163](https://github.com/jorisgillis/agentic-blind-date/issues/163)).

| Page | Route / template | Theme passed |
|------|------------------|--------------|
| Landing | `/` — `landing.html` | `.PublicTheme` (section 5) |
| Public event recap | `/recap` — `event-recap.html` | `.PublicTheme` (section 5) |
| Big Screen | `screen.html` | `.PublicTheme` (section 5) |
| Meet | `/user/m/{id}` — `meet.html` | viewer's theme via `.ThemeName` (see below) |
| Personal recap | `/user/recap/{token}` — `recap.html` | `{{themeCSS .Me.Theme}}` — the token owner's theme |
| Onboarding | `/user/onboard/{id}` — `onboard.html` | `{{themeCSS .Theme}}` — the Participant's own theme |
| Host console | `/admin`, `/admin/slides`, `/data` | `{{themeCSS ""}}` — default, no picker |

### The meet page: the viewer-theme rule

**If the viewer is a logged-in Participant, the meet page follows the
viewer's theme; an anonymous viewer gets the default.** The page is *about*
the target's persona card (which keeps its palette color per section 4),
but the phone in hand belongs to the viewer — their theme surrounds the
target's business card.

Mechanism detail that the code forces: `Meet` renders with an ad-hoc
`map[string]any` in which `Me` is a **possibly-nil** `*Participant`. Field
access on a nil pointer errors at template-execution time, and `{{if .Me}}`
branching violates the one-line rule. So the handler, not the template,
resolves the name:

```go
themeName := ""
if me != nil {
    themeName = me.Theme // themeByName absorbs ""
}
h.render(w, "meet.html", map[string]any{
    "Target": target,
    "Me":     me,
    // ...
    "ThemeName": themeName,
})
```

and the template gets exactly one head line: `{{themeCSS .ThemeName}}`. A
missing/empty key renders the default — anonymous viewers need no special
case in the template.

### The personal recap: a scope correction

`recap.html` at `/user/recap/{token}` is **not** a dark page today — it
renders the owner's persona color as the full-page background with
`color-scheme: light`. It is reached by unguessable token, not by session,
yet it is unmistakably a *personal* page ("the evening is theirs to keep"):
the handler already resolves the owner as a full `*Participant`. It joins
the personal-page family — it follows the **token owner's** theme, not the
unauthenticated default. Its conversion out of the persona-color background
belongs to sections 4 and 7. No picker here either; the token is a
read-only keepsake link.

### Onboarding

`Onboard` renders `onboardPage{Participant: p, …}` — the Participant is
loaded by path id and the cookie was just set by `Join`, so the page
carries a real Participant record: `{{themeCSS .Theme}}` (the default until
they choose). No picker on the onboarding page: the participant is
mid-pipeline, and Settings with its picker is only reachable on the
personal pages.

### Keyframes that stay hardcoded

`meet.html`'s `pop-in`/`meet-pulse` keyframes animate the emerald confirm
pulse — a status color, not a theme role — and stay hardcoded.

---

## 7. The conversion pattern

Source: [#166](https://github.com/jorisgillis/agentic-blind-date/issues/166).

The pattern: every hardcoded Tailwind color class on **theme chrome**
becomes a `--color-*` variable reference (section 2's mechanism, section
1's ten roles); **identity and status colors stay hardcoded** (section 4's
business-card rule); the landing page's inline walkthrough CSS converts
**fully** to variables (section 8).

### The mapping table (locked)

Gray text collapses two ways: **strong grays → `--color-text`** (they were
primary text on the light persona-colored bodies), **muted grays →
`--color-text-muted`** (the 300–700 band collapses into one role — some
one-step lightening/darkening at the extremes is the accepted cost of two
text roles).

| Hardcoded class | Converts to |
|---|---|
| `bg-gray-950` | `bg-[var(--color-bg)]` |
| `bg-gray-900` | `bg-[var(--color-surface)]` |
| `bg-gray-800` | `bg-[var(--color-surface-alt)]` |
| `bg-gray-700` / `bg-gray-700 hover:bg-gray-600` | `bg-[var(--color-surface-alt)]` + `hover:bg-[var(--color-surface-hover)]` |
| `hover:bg-gray-700` / `hover:bg-gray-600` | `hover:bg-[var(--color-surface-hover)]` |
| `text-white` | `text-[var(--color-text)]` on neutral surfaces; `text-[var(--color-accent-text)]` on an accent background; **stays hardcoded** on a status-color button (see exceptions) |
| `text-gray-200` / `text-gray-300` | `text-[var(--color-text)]` / `text-[var(--color-text-muted)]` |
| `text-gray-900` / `text-gray-800` | `text-[var(--color-text)]` |
| `text-gray-700` / `text-gray-600` / `text-gray-500` / `text-gray-400` | `text-[var(--color-text-muted)]` |
| `hover:text-gray-300` / `hover:text-gray-400` | `hover:brightness-110` (same lighten-on-hover move as the accent buttons) |
| `border-gray-900` / `border-gray-800` / `border-gray-700` / `border-gray-600` | `border-[var(--color-border)]` (single border role) |
| `placeholder-gray-500` | `placeholder-[var(--color-text-muted)]` — it converts, it does not stay gray-500 (gray-500 is 3.04:1 on gray-800 today and 4.39:1 on Daylight's gray-100) |
| `bg-indigo-600` | `bg-[var(--color-accent)]` |
| `bg-indigo-500` (progress fills, the pulse dot) | `bg-[var(--color-accent)]` — the indigo family collapses onto the accent role; it has no per-theme variance |
| `hover:bg-indigo-500` | `hover:brightness-110` |
| `hover:bg-indigo-800` | `hover:bg-[var(--color-accent)]` + `hover:text-[var(--color-accent-text)]` on the inner text (the darken move) |
| `text-indigo-400` / `text-indigo-300` / `text-indigo-800` / `text-indigo-700` | `text-[var(--color-accent-muted)]` (indigo-400 in Midnight / indigo-600 in Daylight) |
| `hover:text-indigo-300` | `hover:brightness-110` on `text-[var(--color-accent-muted)]` |
| `text-indigo-600` (checkbox checked color) | `text-[var(--color-accent)]` |
| `focus:ring-indigo-500` / `ring-indigo-500` | `focus:ring-[var(--color-accent)]` / `ring-[var(--color-accent)]` |
| `focus:border-indigo-500` / `hover:border-indigo-500` | `focus:border-[var(--color-accent)]` / `hover:border-[var(--color-accent)]` |
| `from-indigo-500` (match meter gradient) | `from-[var(--color-accent)]` — its `to-pink-500` half stays (match color) |

### Exceptions — classes that never convert

1. **Persona palette classes and their companion text**: `{{.PersonaColor}}` /
   `{{.Match.PersonaColor}}` / `{{.Target.PersonaColor}}` chip and card
   backgrounds, `{{textColor …}}`, the hardcoded `text-gray-9xx`
   companions on persona-colored surfaces, and `paletteHex()`/`colorMap`
   hexes on the Big Screen. The GitHub badges' pastel classes
   (`bg-yellow-200`, `bg-amber-200`, … from `handlers.go`'s `badges`) and
   their `text-gray-800` companion follow the same rule: text on a fixed
   non-theme color never follows the theme.
2. **Status colors, never, as complete units** (background, white text,
   hover, ring, shadow together): green/emerald = success, meetings,
   on-stage (`bg-green-700 hover:bg-green-600`,
   `bg-emerald-600 hover:bg-emerald-500`, `bg-emerald-500/10`,
   `border-emerald-600/50`, `shadow-emerald-500/50`, `ring-green-600`,
   `text-green-800`, `text-green-400`, the `meet-pulse` keyframes); red =
   destructive (`bg-red-900`, `hover:text-red-*`, `text-red-*`); pink =
   match (`to-pink-500`, `bg-pink-600 hover:bg-pink-500`, `text-pink-400`,
   `text-pink-800`); purple = show & tell stage (`bg-purple-600
   hover:bg-purple-500`, `text-purple-300`, `border-purple-500`); amber /
   yellow = trophy and debug state (`text-amber-400`, `text-amber-800`,
   `text-yellow-300`, `text-yellow-400`). These are semantic, not chrome.
3. **The Big Screen's D3 graph**: edge hexes (`#22c55e` meeting, `#ec4899`
   match, `#3b82f6` potential), node persona hexes and fallbacks
   (`#6b7280`, `#9ca3af` name fallback, `#c084fc`), confetti colors —
   locked fixed by the Public theme decision. **Exception to the
   exception**: the node *stroke* `#111827` and label *fill* `#9ca3af` are
   chrome (surface/text-muted), so the script reads them from the injected
   variables at runtime —
   `getComputedStyle(document.documentElement).getPropertyValue('--color-surface')`
   and `…--color-text-muted` — one source of values, no duplicated hexes.
   The error banner's `#7f1d1d`/`#fee2e2` stays (status).
4. **Shadows**: `shadow-lg`, `shadow-md`, black `rgba()` shadows — dimming
   is theme-invariant.
5. **Non-template emitters**: `eventstate.go`'s `ButtonClass` pink/purple
   variants stay (status); its gray variant `bg-gray-700
   hover:bg-gray-600` converts to `bg-[var(--color-surface-alt)]
   hover:bg-[var(--color-surface-hover)]` — one Go string change, same
   mapping.

### Opacity modifiers — collapse to opaque roles

Three findings drive this: Tailwind's alpha modifier cannot compose with a
`var()` arbitrary value (`bg-[var(--color-surface)]/90` generates nothing
usable with the CDN); the ten roles deliberately include no translucent
surfaces; and nearly every translucent class today is an **artifact of the
persona-colored page bodies** (a translucent black or white over a light
persona color reads as "one step off the body" — once bodies are theme
backgrounds, `bg-black/15` over gray-950 is invisible; these were never
really translucency, they were the old surface ramp).

| Translucent class | Converts to | Why |
|---|---|---|
| `bg-black/15` (the panel idiom, 20× in `fragments.html`, all of `meet`/`recap`/`wait` panels) | `bg-[var(--color-surface)]` | it was the card color |
| `bg-black/10` (soft washes: More drawer, slides PDF button) | `bg-[var(--color-surface-alt)]` | one step off the background |
| `bg-black/20` (chips, skeleton bars, progress tracks, upcoming phase dots) | `bg-[var(--color-surface-alt)]` | nested on surface panels — the nested idiom |
| `bg-black/5` (empty mission tile) | `bg-[var(--color-surface-alt)]` | page chrome |
| `bg-white/60` (inputs), `bg-white/40` (exchange panels) | `bg-[var(--color-surface-alt)]` | inputs are surface-alt |
| `bg-white/80` (dossier lock overlay) | `bg-[var(--color-surface)]` | a floating card |
| `border-black/10`, `ring-black/10`, `divide-black/15`, `decoration-black/30` | `border-[var(--color-border)]`, `ring-[var(--color-border)]`, `divide-[var(--color-border)]`, `decoration-[var(--color-border)]` | the divider/ring role |
| `hover:bg-black/20` | `hover:bg-[var(--color-surface-hover)]` | the hover pair |
| `bg-gray-900/50` + `opacity-40` (pipeline inactive steps) | `bg-[var(--color-surface)]` + keep `opacity-40` | element-level opacity composes fine with variables — keep it |
| `hover:bg-gray-900/50` (data table row) | `hover:bg-[var(--color-surface)]` | half-step collapses to the step |
| `bg-gray-800/60` (admin queue items) | `bg-[var(--color-surface-alt)]` | flattening accepted on the console |
| `bg-gray-900/90` (Big Screen HUD panels ×3) | `bg-[var(--color-surface)]` | 90% is visually opaque; the glass effect is not load-bearing |
| `bg-gray-950/80` (presenter scrim) | `bg-[var(--color-bg)]` | the scrim goes **opaque** — presenter mode wants focus, not a ghost of the graph |
| `bg-indigo-900/40` / `bg-indigo-900/50` + `border-indigo-700` (+ `text-indigo-100`) (pipeline active step, admin notices) | `bg-[var(--color-surface-alt)]` `border-[var(--color-accent)]` (+ `text-[var(--color-accent-muted)]`) | one "accent-tinted panel" pattern: surface-alt body, accent border carries the active signal, accent-muted text reads on it in both themes |
| `bg-indigo-200/60` + `border-indigo-400` + `text-indigo-900` (wait page topic chips) | same accent-tinted chip pattern: `bg-[var(--color-surface-alt)]` `border-[var(--color-accent)]` `text-[var(--color-accent-muted)]` | the light-theme-only chip idiom cannot survive Midnight |
| `bg-emerald-500/10`, `border-emerald-600/50`, `shadow-emerald-500/50` | stay hardcoded | status color, exception 2 |

`color-mix()` appears only in the walkthrough style block (section 8),
never in a Tailwind class.

### The full per-file inventory

Every page template also gets its one `{{themeCSS …}}` head line; the
theme source per page is section 6. **Judgment call, stated plainly**: the
host console (`admin.html`, `admin-slides.html`, `data.html`) converts
too, even though it is "not a themed surface" — that phrase governs the
*injection* (default theme, no picker), not the conversion: the console's
queue fragments live in the shared `fragments.html`, partial conversion
invites drift, and under the Midnight default the console renders
pixel-identical to today.

| File | Theme source | What converts |
|---|---|---|
| `landing.html` | `.PublicTheme` | body; explainer card (`bg-gray-900 border-gray-800`); both join inputs (surface-alt, placeholder, focus accent); checkbox; join button; `text-gray-300/400/500/600`; **the walkthrough style block in full** (section 8) |
| `walkthrough.html` | (included by landing) | 6× `text-white` → text, 6× `text-gray-400` → text-muted — markup only, colors live in `landing.html` |
| `onboard.html` | `.Theme` | body; `text-gray-400/500/600` → text-muted |
| `wait.html` | `.Participant.Theme` | body (persona color → themed, section 4); More drawer; Q&A and GitHub panels; topic chips (accent-tinted chip pattern); `text-gray-500/600/700/800/900`; badges stay |
| `match.html` | `.Me.Theme` | body; `{{textColor}}` wrapper deleted (section 4); drawer; share-QR panel; same gray mapping |
| `meet.html` | viewer's theme / `""` anonymous | body; wrapper deleted; status panels (`bg-black/15` → surface); anonymous meet-code form; `bg-indigo-600` "That's me" button → accent; **the whole emerald confirm unit stays** |
| `recap.html` | `.Me.Theme` (token owner) | body; wrapper deleted; all `bg-black/15` panels; `text-indigo-700` repo link → accent-muted; `text-amber-800` trophy stays; persona chips stay |
| `fragments.html` | inherited (fragments never inject) | all 14 defines. Participant-page defines: ~20 `bg-black/15` → surface, ~31 `border-black/10` → border, 7 `bg-white/60` + 2 `bg-white/40` → surface-alt, 8 `placeholder-gray-500`, 9 `bg-indigo-600` + 8 `hover:bg-indigo-500` buttons → accent + `brightness-110`, the one `hover:bg-indigo-800` answer button → hover accent + accent-text, 24 `text-gray-900`/8 `text-gray-800` → text, 31 `text-gray-700` + 15 `text-gray-500` + 11 `text-gray-600` → text-muted, dossier `text-indigo-800` → accent-muted, mission `bg-black/5` → surface-alt, skeletons `bg-black/20` → surface-alt. Console defines (showtell-queue, questions, board-queue): `bg-gray-900`/`gray-800/60` → surface/surface-alt, `hover:bg-gray-600` ×4 → surface-hover, `text-white` queue text → text, `text-gray-300/400/500` → text-muted; green/pink/purple/red action buttons stay. Persona chips everywhere stay |
| `fragment-persona-card.html` | inherited | the identity block becomes the persona-colored business card (section 4 — `bg-white/20` tile goes, ring rides with the block); tagline's `text-gray-700` stays (on the persona color); "This is you" and the QR section are outside the card and convert (`text-gray-600` → text-muted, `text-gray-800` → text) |
| `fragment-phase-header.html` | inherited | panel → surface/border; `bg-indigo-600` segments, dots, badge → accent (+ accent-text); upcoming `bg-black/20` → surface-alt; gray text mapping; emerald on-stage badge stays |
| `fragment-phase-content.html` | inherited | match-reveal hero; meter track → surface-alt, `from-indigo-500` → accent (`to-pink-500` stays); panels → surface/border; `text-pink-800` find-them stays; copy rewording per section 4 |
| `screen.html` | `.PublicTheme` | body; 3 HUD panels → surface/border; scrim → bg (opaque); presenter card → surface (purple border/hype stay); footer; `text-indigo-400` repo link → accent-muted; JS node stroke/label read `--color-surface`/`--color-text-muted` via `getComputedStyle`; edges, persona hexes, confetti stay |
| `admin.html` | `""` (default) | notice (accent-tinted panel pattern); stats panel; reset button → surface-alt/surface-hover; `text-indigo-400`/`hover:text-indigo-300` links → accent-muted + brightness; gray link hovers; `ButtonClass` gray variant converts in `eventstate.go` |
| `admin-slides.html` | `""` | notice; upload form (`bg-gray-950` inputs → bg, `file:bg-gray-800` → surface-alt); `text-indigo-300/400` → accent-muted; `hover:text-red-400` stays |
| `data.html` | `""` | 7 `text-indigo-400` links + `text-indigo-300` handles → accent-muted; table borders/hover; `border-gray-900` rows → border; `text-gray-300` → text-muted; persona swatch stays; yellow/green/red status columns stay |
| `event-recap.html` | `.PublicTheme` | body; 8 stat/section cards → surface/border; `text-gray-200` → text; `text-indigo-400` repo links → accent-muted; persona chips stay |

---

## 8. The landing page walkthrough

Sources: the variable set from
[#166](https://github.com/jorisgillis/agentic-blind-date/issues/166), the
per-theme values from
[#169](https://github.com/jorisgillis/agentic-blind-date/issues/169).

The style block in `landing.html` (lines ~10–183) converts fully — the
Public theme decision. **16 distinct hexes** exist in the block. Rule: a hex
whose Midnight value equals a theme role value **aliases that role's
variable**; the rest get **dedicated walkthrough variables**, named
`--color-walk-<tailwind-source>`. They are injected by the same `themeCSS`
function (the Theme struct gains the walkthrough values as a per-theme
`map[string]string` keyed by the variable tail — `walk-gray-600`, …; the
*names* below are the contract either way).

The walkthrough's mockup canvas flips with the theme: every aliased surface
hex (phone screen, rows, cards, scene gradient, stamp rest) takes the
Daylight role values, so in Daylight the six scenes render as **light
mockups** (white phone, white rows, gray-100 cards, a `#f3f4f6 → #ffffff`
scene gradient). Every dedicated value below was chosen against that light
canvas — Daylight is not "the dark hexes copied onto a light page" but the
same illustration redrawn in the light idiom.

### The complete table — all 16 conversions

| Variable | Midnight | Daylight | Notes |
|---|---|---|---|
| `--color-surface` (alias, from `#111827`) | `#111827` | `#ffffff` | phone screen, rows, stamp rest, scene gradient end. Also the q-bubble "?" glyph color — see the amber row and the contrast table |
| `--color-surface-alt` / `--color-border` (alias, from `#1f2937`) | `#1f2937` | `#f3f4f6` / `#e5e7eb` | picked by usage: bubble/typing/card backgrounds vs line/row/scene borders |
| `--color-surface-hover` (alias, from `#374151`) | `#374151` | `#e5e7eb` | phone notch, join pill bg, card borders, platform |
| `--color-accent` (alias, from `#4f46e5`) | `#4f46e5` | `#4f46e5` | the accent has no per-theme variance; tile bg, stamp highlight, answer bubble border |
| `--color-accent-muted` (alias, from `#818cf8`) | `#818cf8` | `#4f46e5` | indigo figure, avatar A, node A, tile borders, gradients |
| `--color-walk-gray-600` | `#4b5563` | `#4b5563` | **identical** — mid-gray structure lines (skeleton bars, phone frame, pill borders, audience dots, stamp-rest border) read as darker-than-surface on light mockups exactly as they read lighter-than-surface on dark ones |
| `--color-walk-gray-500` | `#6b7280` | `#6b7280` | **identical** — typing dots; 3.04:1 on the Midnight bubble, 4.39:1 on Daylight's, visible in both |
| `--color-walk-indigo-300` | `#a5b4fc` | `#4f46e5` | the "answer text line" bars — **darkens with the same move as `--color-accent-muted`** (light indigo in Midnight, indigo-600 in Daylight), because its carriers flip light: the join pills (surface-hover `#e5e7eb`), the answer bubble, and the persona/mission/recap cards (surface-alt `#f3f4f6`). Treated as text simulation; ≥5:1 on every carrier in both themes |
| `--color-walk-indigo-500` | `#6366f1` | `#6366f1` | **identical** — join button, progress fill, row-highlight border, and the highlight glow's `color-mix` base. The indigo family has no per-theme variance; indigo-500 holds on both canvases (3.97:1 on the Midnight phone screen, 4.47:1 on Daylight's) |
| `--color-walk-indigo-900` | `#312e81` | `#e0e7ff` | the answer bubble — **deep indigo becomes indigo-100**: the light-theme "me bubble" idiom, an indigo-tinted fill with the (theme-invariant) indigo-600 border carrying definition. Keeps the walkthrough's depth as a distinct layer on the white chat canvas |
| `--color-walk-indigo-950` | `#1e1b4b` | `#eef2ff` | the row-highlight wash — **indigo-950 becomes indigo-50**: the same one-step-off-the-row subtlety (1.11:1 vs the row in Midnight, 1.12:1 in Daylight); the indigo-500 border and glow carry the highlight, as today |
| `--color-walk-fuchsia-400` | `#f0abfc` | `#c026d3` | figure B, avatar B, node B, gradients — **darkens to fuchsia-600**: the indigo persona fill automatically deepens to indigo-600 via the accent-muted alias, so its pink partner takes the matching step or washes out beside it (pale `#f0abfc` is 1.9:1 on the light scene; `#c026d3` is 4.3:1). Also the node-B glow's `color-mix` base |
| `--color-walk-emerald-300` | `#6ee7b7` | `#059669` | the presenter figure — **darkens to emerald-600**, same reasoning as fuchsia (pale mint is 2.0:1 on the light stage; `#059669` is 3.4:1) |
| `--color-walk-amber-400` | `#fbbf24` | `#b45309` | the question bubble and handshake spark — **darkens to amber-700, forced by contrast, not aesthetics**: the "?" glyph's color is `#111827`, which locks to the `--color-surface` alias, i.e. **white in Daylight**. White on amber-400 is 1.70:1; amber-700 clears AA at 5.02:1 (amber-600 fails at 3.18:1). The spark inherits the same value and reads fine at 4.56:1 on the light scene |
| `--color-walk-green-500` | `#22c55e` | `#22c55e` | the recap keep-check badge — **identical**: success green is a status color, and it passes as-is |
| `--color-walk-green-950` | `#052e16` | `#052e16` | the keep-check "✓" glyph — **identical**: the pair is already AA in both themes (6.54:1), the one persona-content pair that needs no Daylight move |

Derived values need no decisions: the three `color-mix()` glows compose
from variables above — node A from `--color-accent-muted`, node B from
`--color-walk-fuchsia-400` (22% each), the row highlight from
`--color-walk-indigo-500` (35%) — so Daylight gets soft indigo/pink halos
on the light canvas automatically. The two black `rgba()` shadows stay
hardcoded (exception 4). The `lwStamp` keyframe rest state and the
`.lw-row-scene` gradient flip via their aliases; Daylight gets white tiles
on the gray-100 card — the mirrored subtlety of today's
gray-900-on-gray-800.

### Contrast validation (WCAG 2.1 relative luminance)

The walkthrough's text-bearing pairs, both themes. The two glyph pairs
("?" and "✓") are `aria-hidden` mockup text and the bars simulate text —
validated at the 4.5:1 AA bar anyway. **Every pair passes.**

| Pair | Midnight | Daylight |
|---|---|---|
| q-bubble "?" glyph: `--color-surface` on `--color-walk-amber-400` | 10.63:1 | 5.02:1 |
| recap "✓" glyph: `--color-walk-green-950` on `--color-walk-green-500` | 6.54:1 | 6.54:1 |
| answer text bar: `--color-walk-indigo-300` on join pill (`--color-surface-hover`) | 5.17:1 | 5.08:1 |
| answer text bar: `--color-walk-indigo-300` on answer bubble (`--color-walk-indigo-900`) | 5.73:1 | 5.10:1 |
| answer text bar: `--color-walk-indigo-300` on persona/mission/recap card (`--color-surface-alt`) | 7.36:1 | 5.71:1 |
| row title (`--color-text`) on highlighted row (`--color-walk-indigo-950`) | 15.99:1 | 15.86:1 |
| row caption (`--color-text-muted`) on highlighted row | 6.30:1 | 6.76:1 |
| row title on rest row (`--color-surface`) | 17.74:1 | 17.74:1 |
| row caption on rest row | 6.99:1 | 7.56:1 |

Decorative fills, reported not gated: the Daylight values generally
*improve* visibility — skeleton bars go from 1.94–2.35:1 to 6.87–7.56:1,
the join button from 3.97:1 to 4.47:1, the tiles from 2.33:1 to 5.71:1.

---

## 9. The theme picker

Source: [#167](https://github.com/jorisgillis/agentic-blind-date/issues/167).

### Location: last block inside the settings card, below the contact form

The picker is a new sub-section **inside `fragment-settings.html`**, below
the contact-channels form — the last block of the `#settings` card,
separated from the form by the drawer's divider idiom (`border-t … pt-3`).
The card's existing heading and description describe the contact form; a
theme block wedged between them and their form would split the section's
narrative. Below also keeps the "✅ Saved" confirmation directly under the
Save button where the user's eye is.

The sub-section has its own label in the card's label idiom plus a
one-line muted description that doubles as the no-preview affordance:

```html
<p class="… font-bold text-xs uppercase tracking-wide">🎨 Theme</p>
<p class="… text-xs">One click saves — the page reloads in its new look.</p>
```

**Surfaces**: wherever the settings card renders — the "More" drawer of
`wait.html` and `match.html`. `onboard.html` does not embed the settings
fragment, so the onboarding page gets no picker; a Participant first sees
it after onboarding, on their wait page. (The onboarding page still honors
a stored theme via its `{{themeCSS .Theme}}` head line.)

### The swatch — the canonical visual

One swatch per entry of the `themes` slice, iterated in registry order —
the slice's stable order is the picker's order. **Circles**, not rounded
rectangles. Structure per theme:

```html
{{range .Themes}}
<form hx-post="/user/theme/{{$.Me.ID}}" class="text-center">
  <input type="hidden" name="theme" value="{{.Name}}">
  <button
    type="submit"
    aria-label="{{.Label}}"
    {{if eq .Name $.Me.Theme}}disabled{{end}}
    {{if eq .Name $.Me.Theme}}class="block rounded-full ring-2 ring-[var(--color-accent)]"{{else}}class="block rounded-full"{{end}}
  >
    <span
      class="flex h-9 w-9 items-center justify-center rounded-full"
      style="background: {{.Background}}; border: 2px solid {{.Border}}"
    >
      <span class="h-3 w-3 rounded-full" style="background: {{.Accent}}"></span>
    </span>
  </button>
  <p class="text-xs mt-1 {{if eq .Name $.Me.Theme}}font-bold text-[var(--color-text)]{{else}}text-[var(--color-text-muted)]{{end}}">{{.Label}}</p>
</form>
{{end}}
```

Locked details, precisely enough for the admin row (section 5) to reuse:

- **Sizing**: swatch circle `h-9 w-9` (36px), centered accent dot
  `h-3 w-3`, row `flex gap-3`, label `text-xs` centered under its swatch.
- **Fill/border/dot are inline `style` attributes from the Theme struct's
  hex values** — never page CSS variables. A swatch's whole job is to show
  *the other theme's* literal colors; `--color-*` would render every
  swatch in the current theme. Inline styles carrying registry hexes is
  the same move `themeCSS` makes.
- **Highlight = a ring, not a checkmark**: the current theme's button gets
  `ring-2 ring-[var(--color-accent)]` (the page's accent), and its label
  goes bold in `--color-text`; other labels sit in `--color-text-muted`.
  No checkmark overlay, no per-theme border highlight. The swatch's own
  `border: 2px solid {{.Border}}` shows the theme's border role —
  decorative, as everywhere.
- **Labels from the registry's `Label` field** ("Midnight", "Daylight") —
  under each swatch, not beside it; the row stays compact inside the
  drawer card.

### Interaction: one form per swatch, against the locked handler

The write path is section 3's and is not reopened here: `POST
/user/theme/{id}` → `SaveTheme` → `ParticipantStore.Change` →
**`HX-Redirect: h.destination(p)`, empty body**. No fragment response can
change the page's colors — only a re-rendered `<head>` can. The picker's
only job is to post the choice.

- **One `<form>` per swatch** with a hidden `theme` input, each posting to
  `/user/theme/{{$.Me.ID}}` — the repo's every-POST-is-a-form-with-named-inputs
  idiom. `hx-vals` is used nowhere in the codebase and is not introduced
  for this.
- **No `hx-target`, no `hx-swap`**: the response is `HX-Redirect` with no
  body, so HTMX performs the full-page navigation to the Participant's
  canonical page (`wait` or `match`, `navigation.go`), which re-renders
  the `<head>` with the new theme and the picker's new current-marking in
  one stroke.
- **The swatch forms sit outside the contact-channels `<form>`** (nested
  forms are invalid HTML): the order inside `#settings` is heading,
  description, contact form, "✅ Saved", then the theme block.
- **The current theme's button is disabled.** Re-saving the current theme
  is idempotent but not free: the full reload closes the "More" drawer and
  reconnects the stream. Disabled-for-inapplicable is the codebase idiom;
  the ring already marks it current.
- **No new validation surface**: the picker can only submit registry names
  (it is built from the registry), and the handler stores the trimmed
  value as-is (the lenient rule — `themeByName` absorbing unknown names
  *is* the validation). An unknown stored name simply matches no swatch:
  nothing is marked current, and the page renders the default — harmless
  by construction.

### Refresh behavior: no preview, one full reload

Settled, stated plainly: click → save → full-page navigation via
`HX-Redirect`. No live preview (switching is cheap, so previewing buys
nothing), no fragment swap, no client-side variable rewrite. The
description line tells the user exactly what will happen. The drawer being
closed after the reload is the same cost every phase redirect on these
pages already pays; not worth persisting.

### Data plumbing: one key in `settingsData`

`settingsData` (`handlers.go`) adds one key: `"Themes": themes` — the
slice registry itself, next to `"Channels"`. That single change covers
every render path of the fragment: the `Settings` GET, the `SaveSettings`
POST re-render, and the card registry's `Data` for full `wait.html` /
`match.html` renders (`cards.go`) — all three already call
`settingsData(p)`. The current theme needs no plumbing: it is
`{{$.Me.Theme}}`, already on the `Me` participant.

No stream signature, no `refresh` wiring for the settings card: a theme
change only ever arrives through the picker's own post, whose response is
the full-page redirect — there is nothing for the stream to announce.

### The admin row reuses this visual

The admin console's "Public theme" swatch row mirrors this picker: same
circle (36px, theme `Background` fill, 2px `Border`, centered `Accent`
dot), same `gap-3` row, same label-under-swatch, same
ring-plus-bold-label current-marking. Differences: the admin row posts as a
plain form POST (303 to `/admin`), and the console is not a themed surface,
so its ring uses a hardcoded class (`ring-2 ring-indigo-500`) instead of
a variable. The reusable spec is the swatch's structure, sizing, highlight
treatment, and labels as defined above.

---

## 10. Rollout

Source: [#168](https://github.com/jorisgillis/agentic-blind-date/issues/168).

The headline: **there is no migration step.** The schema change (section
3) is the whole rollout on the data side — SQLite's `ADD COLUMN … NOT
NULL DEFAULT` backfills every existing row with the default at boot.
Everything here is about what people *see*, what data survives, and what
to say.

### Existing Participants at rollout: Midnight, no action required

- Every existing row reads `theme = 'midnight'` the instant the theming
  build boots, via the column default. No backfill statement, no
  maintenance window, no downtime beyond the normal deploy — `NewDB` runs
  the migrations slice on every start, and its entries run with errors
  ignored (a second boot re-runs the ALTER, it fails with "duplicate
  column", nobody cares).
- The picker appears with **Midnight preselected** — `themeByName` renders
  their stored name, the picker marks it current. Switching to Daylight
  is opt-in, one tap. Nobody is asked to configure anything; a Participant
  who ignores the picker entirely is never blocked or nagged.
- Nothing else about a Participant is touched: persona, contact channels,
  meet code, matches, pipeline step, show & tell signups all read exactly
  as before.

### The visible change: the body, and only the body

What an existing Participant sees on their first post-rollout load of
`wait`, `match`, or `recap`:

- The page background flips from their persona color to Midnight's dark
  (`#030712`, today's `bg-gray-950`), and the body text flips from
  `text-gray-900` to white. The `style="color-scheme: light"` and the
  `{{textColor}}` page wrappers go with it.
- **Everything else renders as it does today**, because every Midnight
  role hex is the current hardcoded Tailwind hex: cards, panels, borders,
  indigo buttons, muted text — pixel-identical to today's dark pages. A
  Participant who never touches the picker sees their page change *only*
  in that the persona-colored body becomes the Midnight body.
- **Identity survives on the card**: their persona color, emoji, name, and
  tagline render as the business card — the persona-colored identity
  block — plus the persona chips across all pages and the graph nodes on
  the Big Screen. The color left the walls, not the person.

### Persona colors: no data migration

`persona_color`, `persona_symbol`, the `paletteColor` type, `paletteText`,
and `paletteHex` are unchanged — same columns, same values, same code.
They continue to feed the business cards, chips, mission grid tiles, and
Big Screen hexes per section 4's inventory. Nothing moves, so nothing can
be lost.

### Public pages and the Big Screen: no migration

The Public theme is a `public_theme` key in `event_state`, and an absent
key renders Midnight — which is what `screen.html`, `landing.html`, and
`event-recap.html` already look like today. Existing deployments have no
key, so **public surfaces are visually unchanged at rollout** until the
Host picks Daylight from the console's swatch row.

### Reset and stored theme values

- `EventStore.Reset` deletes whole tables in one transaction — including
  `DELETE FROM participants`. A Participant's theme is a column on their
  row, so **theme values vanish with their owners**: no orphaned data, no
  cleanup, and Reset needs no change. The codebase has no theme data
  outside the `participants` table.
- A new event starts with zero participants; every new registration goes
  through `CreateParticipant`, which omits the column and is covered by
  the DDL default — **every fresh Participant is on Midnight until they
  choose**. Same experience for the new cohort as for the migrated one.
- **The Host's `public_theme` choice survives a Reset.** Reset rewrites
  only the `phase` key; other `event_state` keys are untouched. Judgment
  call, stated plainly: the Public theme is a console preference about the
  venue, not event data, and a Host running a second event in the same
  dim room should not be re-picking it.
- **No data-quality concern exists by construction**: no query looks up
  Participants by theme, and `themeByName` absorbs any unknown stored name
  at render — a stale value from a future registry change degrades to
  Midnight, never to a broken page.

### Rollback

If theming is rolled back: revert the templates and registry (the
conversion is template-and-registry code, one revert), then

```sql
ALTER TABLE participants DROP COLUMN theme;
DELETE FROM event_state WHERE key = 'public_theme';
```

`DROP COLUMN` is an established idiom here — the migrations slice already
contains `ALTER TABLE participants DROP COLUMN extra_answers`. Personal
pages revert to persona-color bodies exactly as they are today. **No data
loss**: theme is the only new data, and a Participant's Daylight choice
dying with the column is acceptable — it is a cosmetic preference, not a
fact about them. The `POST /user/theme/{id}` route disappears with the
revert; a stale form posting to it 404s harmlessly.

### Testing

- A migration test in the `TestDB…OldExtraAnswersColumn` shape (`db_test.go`):
  open a DB, close it, reopen it (the slice runs twice — idempotency), and
  assert a Participant created before the theme column existed reads
  `Theme == "midnight"` after reopening — the DEFAULT backfill is the
  migration, and this test pins it. Schema inspection via
  `pragma_table_info` is available if a column-presence assertion is
  wanted.
- Render-side behavior (unknown stored name → Midnight) is already
  `themeByName`'s fallback; no migration test duplicates it.

### What the rollout needs to communicate

- **To Participants, one sentence of host patter is enough**: "Your page
  now defaults to dark — your color is on your card. Want light mode? It's
  in Settings." Nothing to do, nothing to redo.
- **The find-them hint changes with the rollout** (section 4): the
  shipped copy must point at what still carries the color — their card and
  chips ("look for 🦊 — their card is teal"). The Host's "find your match
  in the room" guidance at the reveal should say the same: identify people
  by the card on their screen, not by their screen's background. This copy
  change ships with theming, not after it — otherwise the hint is wrong on
  day one.

---

## Implementation order

The spec is fully locked; implementation is mechanical. Natural order:
section 1 (the type and registry) → section 2 (injection) → section 3
(storage) → section 7's inventory, with sections 4–6 and 8–10 in any
order after 3. Every conversion's Midnight values render pixel-identical
to today, so partial rollouts are visually safe per page.

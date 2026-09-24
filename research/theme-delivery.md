# Theme delivery and browser constraints

Research for `.scratch/ui-and-themes/issues/01-theme-delivery-and-browser-constraints.md`.
Sources checked 2026-09-24. Where a claim comes from a bug tracker rather than a spec or official doc, it says so.

## 1. Tokens

### CSS custom properties are the runtime switch
Custom properties declared with `--` always inherit from their parent. If you declare them on `:root` (or on `[data-theme=x]`), every descendant picks up a new value as soon as the selector matches. JS can also set them with `style.setProperty()`. `var()` takes fallbacks. ([MDN: Using CSS custom properties](https://developer.mozilla.org/en-US/docs/Web/CSS/Using_CSS_custom_properties))

A Theme is a block of variables:

```css
:root, [data-theme="classic"] { --color-surface: #fff; --font-display: "Inter"; --radius-card: 1rem; --motion-fast: 150ms; }
[data-theme="neon"]           { --color-surface: #0b0b1a; --font-display: "Orbitron"; --radius-card: 0; --motion-fast: 90ms; }
```

**What this means for Themes:** you define each Theme once as variables on a selector. Switching Themes means changing one attribute, and nothing gets re-rendered.

### Tailwind v4: theme variables become CSS variables and utilities
- Tailwind v4 `@theme` variables "aren't just CSS variables — they also instruct Tailwind to create new utility classes". They are emitted as custom properties on `:root`. `@theme` has to be top level, not nested under a selector. ([Tailwind: Theme variables](https://tailwindcss.com/docs/theme))
- To point a token at a variable that a Theme swaps, use `@theme inline { --color-surface: var(--surface); }`. The utility then compiles to `background-color: var(--surface)`. Without `inline`, a variable resolves where it is defined, not where it is used, which "may resolve to unexpected values". (same source)
- A `data-theme` attribute can also drive variants: `@custom-variant dark (&:where([data-theme=dark], [data-theme=dark] *));`. ([Tailwind: Dark mode](https://tailwindcss.com/docs/dark-mode))

### Tailwind Play CDN
- The app loads `https://cdn.tailwindcss.com`, which is the **v3** Play CDN. It is customised through a runtime `tailwind.config = {...}` object. The v3 docs call it "designed for development purposes only, and is not the best choice for production." ([Tailwind v3: Play CDN](https://v3.tailwindcss.com/docs/installation/play-cdn))
- v3 **can** map utilities to CSS variables. You store the colour as bare channels (`--color-primary: 255 115 179;`) and configure `primary: 'rgb(var(--color-primary) / <alpha-value>)'`. Opacity modifiers like `bg-primary/50` still work. ([Tailwind v3: Customizing colors, Using CSS variables](https://v3.tailwindcss.com/docs/customizing-colors))
- The v4 Play CDN is `https://cdn.jsdelivr.net/npm/@tailwindcss/browser@4`, configured with `<style type="text/tailwindcss">@theme {...}</style>`. Its docs say it is "for development purposes only, and is not intended for production." ([Tailwind: Play CDN](https://tailwindcss.com/docs/installation/play-cdn))
- The Play CDN is a script that generates CSS in the browser after load. That makes it a runtime dependency on a third-party CDN, and styles appear only after the script runs. This is inferred from how it works; the docs do not spell out the reasons.

### Build step: Tailwind CLI
`npx @tailwindcss/cli -i input.css -o output.css --watch`. There is also a **standalone executable** that needs no Node.js ([Tailwind CLI](https://tailwindcss.com/docs/installation/tailwind-cli), [releases](https://github.com/tailwindlabs/tailwindcss/releases/latest)). The output is a static CSS file that Go can embed and serve. The CLI builds only the classes it finds in the templates, so persona classes built dynamically (for example `text-{{.Colour}}-400`) must be written out in full somewhere, or safelisted.

| Option | Runtime theming | Production suitability | Cost |
|---|---|---|---|
| Plain CSS vars + existing CDN utilities | Yes, through `var()` in a small stylesheet or arbitrary values `bg-[var(--surface)]` | CDN still "not for production" | Zero change to the build |
| v3 Play CDN + `tailwind.config` mapping to vars | Yes | Dev-only per docs | Config lives in an inline script |
| v4 Play CDN + `@theme inline` | Yes | Dev-only per docs | Moves to v4 syntax |
| Tailwind CLI (standalone) + `@theme inline` | Yes | Intended for production | One build step; the CSS file is committed or embedded |

**What this means for Themes:** tokens should be CSS custom properties switched by `[data-theme]`, and Tailwind should map its utilities to them. The mechanism works with every option in the table. Replacing the CDN with the standalone CLI is a separate decision, and the docs point that way for production. Persona colours stay as fixed Tailwind `*-400` classes that never reference Theme variables, so every Theme leaves them alone.

## 2. Choice: apply before first paint; survive HTMX and SSE

- Tailwind's docs recommend putting the theme-applying script "inline in `head` to avoid FOUC". The script reads `localStorage` and sets the class or attribute on `document.documentElement` ([Tailwind: Dark mode](https://tailwindcss.com/docs/dark-mode)). A classic (non-module, non-deferred) inline script in `<head>` runs before `<body>` is parsed, so the first paint already uses the right Theme. Pattern:
  ```html
  <script>try{document.documentElement.dataset.theme=localStorage.getItem('theme')||'classic'}catch(e){}</script>
  ```
  Put it before the stylesheet or CDN script. Wrap it in `try` because `localStorage` can throw when storage is blocked.
- For the **Big Screen**, the admin's choice lives on the server. Go can render `<html data-theme="{{.Theme}}">` directly, so no script is needed for first paint. A live change can arrive as an SSE event that a small listener turns into `document.documentElement.dataset.theme = ...`.
- HTMX swaps only touch the target: `outerHTML` means "Replace the entire target element with the response", and `innerHTML` replaces its contents ([htmx: hx-swap](https://htmx.org/attributes/hx-swap/)). `hx-boost` targets `<body>` with `innerHTML` ([htmx: hx-boost](https://htmx.org/attributes/hx-boost/)). The SSE extension either swaps event content into elements marked `sse-swap`, or fires `hx-get` requests through `hx-trigger="sse:<event>"` ([htmx: SSE extension](https://htmx.org/extensions/sse/)). None of these targets `<html>`, so the `data-theme` attribute on it survives every swap and refresh. Because variables are inherited, swapped-in fragments pick up the current Theme with no extra work.
- A full navigation or reload re-runs the head script, so the Theme is reapplied every time.
- Watch out: a server fragment should never include its own `data-theme` or Theme colours. Keep hard-coded colours out of partials, or they will stop following the Theme.

**What this means for Themes:** participant phones use one inline head script reading `localStorage` into `<html data-theme>`. The Big Screen gets its Theme rendered by the server and updated live over SSE. HTMX `outerHTML` and SSE refreshes leave the choice intact, provided fragments style themselves only through tokens.

## 3. Motion and sound

### Autoplay rules (all browsers)
- MDN says autoplay is generally allowed if at least one of these is true: the media is muted or at volume 0, the user has interacted with the site, the site is allowlisted, or a Permissions Policy grants it to an iframe. `play()` rejects with `NotAllowedError` when blocked, and `navigator.getAutoplayPolicy()` can detect the policy in advance. ([MDN: Autoplay guide](https://developer.mozilla.org/en-US/docs/Web/Media/Guides/Autoplay))
- Autoplay of media **and Web Audio** requires **sticky activation**. Sticky activation is set by a trusted `keydown` (not Esc), `mousedown`, `pointerdown` (mouse), `pointerup` (non-mouse) or `touchend`, and it "is not reset" for the rest of the page's life. ([MDN: User activation](https://developer.mozilla.org/en-US/docs/Web/Security/User_activation))
- **Chrome (desktop and Android):** "Muted autoplay is always allowed." Sound is allowed if the user has interacted with the domain, **or** (desktop only) the Media Engagement Index threshold has been crossed, **or** the site was added to the home screen or installed as a PWA. MEI counts playback that is longer than 7 s, audible, in an active tab, and larger than 200x140 px. An `AudioContext` created before a gesture "will be created in the 'suspended' state, and you will need to call resume() after the user gesture." For a kiosk you can launch `--autoplay-policy=no-user-gesture-required`, and the enterprise policies `AutoplayAllowed` and `AutoplayAllowlist` exist. ([Chrome: Autoplay policy](https://developer.chrome.com/blog/autoplay))
- **Safari (macOS and iOS):** "Websites should assume any use of `<video>` or `<audio>` requires a user gesture click to play" ([WebKit: Auto-play policy changes for macOS](https://webkit.org/blog/7734/auto-play-policy-changes-for-macos/)). On iOS, muted `<video>` may autoplay, but media that becomes unmuted without a gesture pauses ([WebKit: New video policies for iOS](https://webkit.org/blog/6784/new-video-policies-for-ios/)).
- **Firefox:** follows the same model in the MDN guide (muted, or user interaction, or an allowlisted site). I could not load Mozilla's own support article (`support.mozilla.org/kb/block-autoplay`) during this research.

### Web Audio unlock pattern
1. Create a single `AudioContext` lazily, or create it early and let it start `suspended`.
2. In the first trusted `pointerup`/`touchend`/`keydown` handler, call `ctx.resume()` and optionally play a zero-gain buffer. Because the activation is sticky, the page can then play sounds later, for example on SSE events, without further gestures.
3. Listen for `statechange` and resume on `visibilitychange`. On iOS Safari the state becomes `"interrupted"` when the user leaves the page, locks the screen or switches tabs, and it "needs to be manually resumed". ([MDN: BaseAudioContext.state](https://developer.mozilla.org/en-US/docs/Web/API/BaseAudioContext/state))

### iOS silent switch: Web Audio vs `<audio>`
- In `"auto"` mode, an `AudioContext` defaults to the session type `"ambient"` and an `HTMLMediaElement` to `"playback"`. The page takes the highest-priority type that is active. ([MDN: Audio Session API](https://developer.mozilla.org/en-US/docs/Web/API/Audio_Session_API), [W3C Audio Session draft](https://w3c.github.io/audio-session/))
- WebKit bug 237322, "webaudio api is muted when the iOS ringer is muted". A WebKit engineer comments there: "By default the type is `ambient` and so audio will be muted if the phone is muted". The fix since iOS 17 is `navigator.audioSession.type = 'playback'`. ([WebKit Bugzilla 237322](https://bugs.webkit.org/show_bug.cgi?id=237322); bug tracker, not a spec)
- So Web Audio respects the silent switch by default, and `<audio>` or `'playback'` plays through it. `'playback'` also pauses other apps' playback audio, while `'ambient'` mixes with it. ([MDN: AudioSession.type](https://developer.mozilla.org/en-US/docs/Web/API/AudioSession/type)). The Audio Session API has "Limited availability" and is not Baseline. In practice it is Safari only.

### `prefers-reduced-motion`
The values are `no-preference` and `reduce`, and it is Baseline since January 2020. It is set by OS settings (iOS: Accessibility > Motion; Android 9+: Remove animations; macOS, Windows and GNOME equivalents). It is available in CSS `@media` and in `matchMedia(...).addEventListener('change', ...)`. ([MDN: prefers-reduced-motion](https://developer.mozilla.org/en-US/docs/Web/CSS/@media/prefers-reduced-motion))

### Can an untouched Big Screen tab play sound?
- **After one click, yes.** Sticky activation lasts for the life of the document. The Big Screen updates through HTMX swaps and SSE without full reloads, so a single "Start" click keeps sound unlocked until the page reloads. Resume the `AudioContext` inside that click.
- **With no click at all:** Chrome desktop allows it only if MEI for the origin has built up, which cannot be relied on for a new or rarely used host, or if the browser runs with `--autoplay-policy=no-user-gesture-required` or an enterprise `AutoplayAllowlist`. Safari and Firefox need a gesture, or the site added to their per-site autoplay allowlist by hand. Use `navigator.getAutoplayPolicy('audiocontext')` where it exists to decide whether to show a "Click to enable sound" overlay.
- **Unverified:** whether sticky activation carries over a full same-origin reload in Chrome. The Chrome docs say "interacted with the domain" without defining how long that lasts. Plan for one click per page load.

**What this means for Themes:** Theme motion should be CSS duration and easing tokens that collapse to near zero under `@media (prefers-reduced-motion: reduce)`. JS-driven animation must check `matchMedia`. Theme sound should go through one shared, lazily unlocked `AudioContext`:
- **Phones:** unlocked on the first tap. Leave it `ambient` so the silent switch is respected, which suits phones in a room. Set it to `'playback'` only for an explicit "sound on" choice.
- **Big Screen:** needs a visible one-time "Start / enable sound" click, or a kiosk launch flag. Every Theme must work with sound silently unavailable.

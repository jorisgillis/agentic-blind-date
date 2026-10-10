---
label: wayfinder:grilling
title: Which tokens make up a Theme, and where motion and sound happen
status: closed
assignee: claude
blocked_by: [01-theme-delivery-and-browser-constraints.md]
---

## Question

Given the research on delivery and browser constraints, what exactly is a Theme made of?

- The named tokens: surfaces, text, accents and states; font families; corner radius and spacing; motion durations and easing.
- The UI moments that get motion or sound: an answer submitted, the Interview done, matched, the Reveal, a new line in the Big Screen ticker.
- Whether sound is opt-in, and how the Big Screen's sound is started.
- How Persona colours stay readable on every Theme's surfaces.

## Resolution

Decided with the human on 2026-09-24:

- **Colour roles (12):**
  - surfaces: page, raised card, translucent overlay
  - text: normal, muted
  - accent: the accent, text on the accent
  - border
  - states: positive, negative, warning
  - highlight: the match and Reveal gradient

  The Persona palette is not part of any Theme.
- **Persona-coloured pages** (wait, Match, Explore) keep the Persona colour as their page background in every Theme. A Theme styles what sits on top (overlays, cards, type, motion, sound), so its overlay tokens must work on every Persona colour.
- **Typography:** display, body and mono families plus a display weight. Each Theme may add at most one web font, served by the app itself (no third-party font CDN).
- **Shape:** corner radius small, medium, large and pill; a border width; one shadow or glow style. Spacing is not themed, so layout stays constant.
- **Motion:** durations fast, normal and slow; easings standard and emphasis. All motion collapses under `prefers-reduced-motion`. The eight moments a Theme can animate:
  - on the phone: an answer submitted, the Interview done, matched (after the Reveal), the Reveal
  - on the Big Screen: a new Participant, a new Match edge, a new ticker line, the Reveal
- **Sound:** a cue is a synthesised tone recipe (waveform, pitch, length) played through the browser's audio engine, with no audio files.
  - Phones: sound is off by default and opt-in. Cues: answer submitted, Interview done, the Reveal / your Match.
  - Big Screen, after the admin's "enable sound" click: a new Match and the Reveal.
  - A Theme may leave any cue silent.
- **Readability:** every Theme keeps a contrast ratio of at least 3:1 between each Persona colour and its page and card surfaces, and readable text on its overlays over every Persona colour. Checked by an automated test on the Theme catalogue.
- **Default Theme:** today's look, with no sound. The human has no strong preference, so this can be revisited when the three looks are prototyped.
- **Admin and data pages** follow the organiser's own device choice, like any phone.

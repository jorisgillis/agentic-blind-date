---
label: wayfinder:prototype
title: What the three Themes look, move and sound like
status: closed
assignee: claude
blocked_by: [02-theme-token-set.md]
---

## Question

Using the decided token set, what are the three Themes? Build a throwaway Theme sampler that shows the landing page, a question, the wait page, the Match page and the Big Screen in each candidate Theme, including motion and sound cues. Then pick the three and their names (for example "default", "terminal", "neon").

## Resolution

Decided with the human on 2026-09-24, after a throwaway prototype of five variants on mock versions of the landing page, a question, the wait page, the Match page and the Big Screen. The prototype is on branch `prototype/themes`: open `prototype/themes.html` in a browser.

- **The three Themes:**
  - **Classic:** today's look, with no sound. It is the default, and the picker calls it Classic, not "Default".
  - **Neon:** deep purple with magenta and cyan, large radii, a double glow, bouncy motion with overshoot, and saw/triangle synth cues.
  - **Paper:** a light Theme with cream and white surfaces, serif headings, soft shadows, gentle motion and soft sine chimes.

  Terminal and Arcade were not chosen.
- **Starting values:** the prototype's token values for Classic, Neon and Paper are the starting point for the build, to be tuned in the real pages.
- **The token set grows from 12 to 14 roles** (an amendment to "Which tokens make up a Theme, and where motion and sound happen"):
  - **Text on a Persona colour:** readable on all five Persona colours, for the Persona-coloured wait, Match and Explore pages.
  - **Persona outline:** drawn around every Persona-coloured element on a Theme surface (avatars, Big Screen graph nodes, badges). It is transparent in dark Themes.
- **Light Themes and Persona contrast:** on Paper every Persona colour is below 3:1 against the surfaces (amber is 1.5). A light Theme therefore draws the Persona outline, and the 3:1 check applies to the outline against the surface, while Persona colours stay unchanged. The Persona-coloured pages are unaffected, because there the Persona colour is the background.

# Manual checklist: Theme motion (#43)

Automated tests cover the generated CSS (motion tokens, the
`prefers-reduced-motion` kill-switch) and that every page includes the
reduced-motion guard. What they cannot cover — because it needs a real
browser rendering a real animation — is checked here by hand, once per
Theme (Classic, Neon, Paper), with and without reduced motion.

## Setup

- macOS/iOS: System Settings → Accessibility → Display → Reduce Motion.
- Android: Settings → Accessibility → Remove animations.
- Desktop Chrome/Firefox: emulate via DevTools → Rendering →
  "Emulate CSS media feature prefers-reduced-motion".

## Phone moments

For each Theme, with reduced motion **off**, then again **on**:

- [ ] Submitting an answer: the question fragment fades out and the next
      one fades in (not an abrupt cut), using the Theme's fast duration.
      With reduced motion on: the swap is instant, no visible fade.
- [ ] The Interview finishes: navigating to the wait page feels like a
      normal page load, no stray animation left over.
- [ ] Getting matched (wait page, before the Reveal): the "You've been
      matched!" panel pops in. With reduced motion on: it just appears.
- [ ] The Reveal (match page load): the persona card and match card pop in
      with the Theme's emphasis easing (Neon should feel bouncier than
      Paper). With reduced motion on: everything appears immediately,
      no scaling or fading.

## Big Screen moments

For each Theme, with reduced motion **off**, then again **on**:

- [ ] A new Participant joins: their node grows in from nothing over the
      Theme's slow duration. With reduced motion on: it appears at full
      size immediately (no grow-in).
- [ ] A new Match happens: the edge between the two nodes pulses
      continuously. With reduced motion on: the edge is present but
      static, not pulsing.
- [ ] A new activity ticker line arrives: the ticker text fades to the
      new line. With reduced motion on: the text just changes.
- [ ] The admin reveals: the whole screen flashes once with the Theme's
      highlight colour. With reduced motion on: no flash, the reveal is
      only visible through the graph and ticker changing.

## Notes

- Neon's overshoot easing and double glow should be visually distinct from
  Classic's plainer transitions and Paper's gentler, no-overshoot ones.
- If any moment still visibly animates with reduced motion on, that is a
  bug: file it against `theme_render.go`'s `motionStyleBlock` or the
  specific JS call site that isn't checking
  `agenticBlindDateReducedMotion()`.

# Manual checklist: The Game Show's Theme moments (#85)

Automated tests cover that each moment has a motion class or transition
(collapsed under reduced motion) and an optional sound cue in every Theme's
token set. What they cannot cover — because it needs a real browser
rendering real animation and sound — is checked here by hand, once per
Theme (Classic, Neon, Paper), with sound on and with sound off.

## Setup

- Phone: `/user/show/{id}` in a Game Show, during an open Round.
- Big Screen: `/bigscreen`, with "🔊 Enable sound" clicked for the sound
  checks.
- Toggle reduced motion the same way as the [Theme motion
  checklist](theme-motion.md).

## The five moments

For each Theme, with sound **on**, then again **off**, and with reduced
motion **off**, then again **on**:

- [ ] **A Round opens** (phone): the answer pad pops in. With sound on,
      Classic is silent, Neon and Paper each play their own tone. With
      reduced motion on, the pad just appears.
- [ ] **The countdown's last 3 seconds** (phone): the seconds counter
      pulses and (Neon/Paper) sounds once, right as it crosses from 4 to 3,
      not on every tick after. With reduced motion on, it stops pulsing but
      the number still counts down.
- [ ] **A Round closes, with the callout** (Big Screen): the majority/minority
      panel pops in and (Neon/Paper) sounds once, only when a *new* callout
      appears — not on every unrelated Big Screen update while the same
      callout is showing.
- [ ] **The matchmaking drumroll** (Big Screen): the "🔮 The agents are
      deciding..." panel pops in and (Neon/Paper) sounds once on the way
      into the matchmaking segment, not on every update while it's showing.
- [ ] **A pair revealed** (Big Screen): the pair's Personas glow in gently
      (not an instant snap) and (Neon/Paper) sound once per pair, as the
      Reveal advances from one pair to the next.

## Notes

- Classic has no sound for any moment (by design, like every other cue):
  confirm it's genuinely silent, not just quiet.
- If a moment's animation or sound repeats on every Big Screen refresh
  instead of once per occurrence, that's a bug in `screen.html`'s
  "previous state" tracking (`lastCalloutKey`, `drumrollEl`'s own
  display style, `lastRevealKey`).

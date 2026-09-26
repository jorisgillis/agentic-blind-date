# Manual checklist: Theme sound (#44)

Automated tests cover the sound module's presence on every phone page, its
absence on the Big Screen, the generated cue data, and that the
play-through-the-silent-switch control exists. Whether sound actually
plays, respects the silent switch, and survives real device quirks needs a
real device — checked here by hand.

## iOS Safari (silent switch)

- [ ] With the phone's silent switch **on** and sound **off** in the app:
      no sound plays when submitting an answer, finishing the Interview,
      or getting matched.
- [ ] Turn sound **on** in the app (tap the 🔇 button). With the silent
      switch still **on**: cues stay silent (ambient audio session
      respects the switch).
- [ ] Tap the 📵 "play through silent switch" button, then trigger a cue
      again, silent switch still **on**: the cue is now audible
      (Safari 17+; on older Safari, `navigator.audioSession` is absent and
      this control has no effect — cues stay silent, which is the safe
      fallback).
- [ ] Flip the silent switch **off**: cues are audible regardless of the
      play-through setting.
- [ ] Reload the page (new page in the flow, e.g. wait → match): sound
      stays on (persisted), and the first tap on the new page unlocks
      audio again without needing to re-tap the toggle.

## Android Chrome

- [ ] Sound off by default; tapping 🔇 turns it on and a cue is audible
      immediately (the tap itself unlocks the AudioContext).
- [ ] No console errors when `navigator.audioSession` is absent (Chrome
      doesn't implement it): the play-through toggle should be inert but
      harmless.
- [ ] Cues for Neon (sawtooth/triangle) and Paper (soft sine) sound
      audibly different from each other and from Classic's silence.

## Desktop Big Screen tab

- [ ] On load, the Big Screen is silent and shows "🔇 Enable sound".
- [ ] Clicking it once changes the label to "🔊 Sound on" and a subsequent
      Match or Reveal is audible.
- [ ] Reloading the tab returns to silent with the button reset — the
      choice is deliberately not persisted (unlike the phone's).
- [ ] Everything else (graph, ticker, Reveal flash) works identically
      whether or not sound was ever enabled.

## Notes

- No feature should ever be blocked or degraded by sound being off,
  unsupported, or blocked by browser autoplay policy — if any of the
  above steps show a broken *feature* (not just silence), that's a bug.

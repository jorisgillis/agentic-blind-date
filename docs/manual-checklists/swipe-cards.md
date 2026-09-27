# Manual checklist: Swipe cards (#80)

Automated tests cover that a two-option Round renders a swipe card (with
tap buttons as a fallback) and that both swiping and tapping record the
same answer through the mux. What they cannot cover — because it needs a
real touchscreen — is the swipe gesture itself, checked here by hand.

## Setup

Open the show status page (`/user/show/{id}`) on a phone while a
two-option Round is open, in:

- iOS Safari
- Android Chrome

## Checklist

- [ ] A left swipe on the card records the left-hand option and the card
      flies off to the left, rotating as it goes.
- [ ] A right swipe records the right-hand option and the card flies off
      to the right.
- [ ] A short drag that doesn't cross the swipe threshold snaps back
      without recording an answer.
- [ ] Tapping either option button records the same answer as swiping it,
      without needing a swipe gesture.
- [ ] Submitting an answer (by swipe or tap) re-renders the pad so the
      Participant can change their answer again while the Round stays open.

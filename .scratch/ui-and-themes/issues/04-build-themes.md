---
label: wayfinder:task
title: Build the Themes
status: closed
assignee: claude
blocked_by: [03-three-theme-looks.md, 07-tailwind-build-step.md]
---

## Question

Build and ship the three Themes, as the map's Notes allow:
- tokens used by every page
- a Theme picker on the Participant's phone, remembered on the device
- the admin's Big Screen Theme picker
- motion and sound as decided
- Persona colours unchanged in every Theme

The build is specced in GitHub issue #36 ("Spec: Themes") and split into these tickets, labelled `needs-triage` until this map's open Theme decisions are made:
- #40 Theme tokens on every page, with one default Theme
- #41 Participants pick one of three Themes on their phone
- #42 The admin picks the Big Screen's Theme
- #43 Theme motion
- #44 Theme sound

This ticket closes when they are done.

## Resolution

Done: #40–#44 are all built and merged, plus a code-review fix batch
(commits through "fix: Apply code-review findings for the Themes batch
(#40-#44)"). Tokens, both pickers, motion and sound all landed, with the
manual checklists the spec called for. The Game Show's own five Theme
moments (#85) build directly on this token set.

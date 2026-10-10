---
label: wayfinder:grilling
title: Spec the chosen UI concept
status: closed
assignee: claude
blocked_by: [05-radical-ui-concepts.md, 02-theme-token-set.md]
---

## Question

Turn the **Game Show** concept, with the Swipe Deck's card animations, into a build-ready spec. See the prototype on branch `prototype/ui-concepts`, concept D, and concept A for the animations. Questions it must answer:

- **Run of show:** who drives it (an admin as host, a timer, or both), and which segments exist (join, rounds, the matchmaking segment, the Reveal)?
- **Matching:** does the show replace Continuous Matching, pairing everyone in the matchmaking segment, or do they coexist? What happens to Participants who arrive late, after the rounds have started?
- **Shared rounds versus the Interview:**
  - Everyone answers the same question at once. What happens to Custom Questions (per GitHub profile) and Extra Questions (Non-GitHub Users)?
  - Which questions become rounds?
  - Is the rest still answered individually?
- **Phone as controller:**
  - the screens and states of the controller
  - where the swipe cards apply
  - how the phone shows "stand up, your Match is…" and switches to the Match's colour
- **Big Screen segments:** what the stage shows in each segment (live answer splits, the matchmaking drumroll, pairs lighting up), and how anonymity holds until the Reveal.
- **Real-time push:** is the current change feed with SSE enough to keep phones and the Big Screen in step within a round, or is something else needed?
- **Adoption:** does it replace the current UI, or run alongside it behind an event setting?
- **Themes:** how it uses the Theme tokens and the eight moments, which likely gain round-specific moments.
- **Domain:** which new terms go into CONTEXT.md (for example Round, Host, Show segment).

Use the codebase-design skill for the modules involved (Interview, Matchmaking, Big Screen, Event State).

## Resolution

Decided with the human on 2026-09-24. This is the build-ready content of the Game Show spec, to be published as a spec issue.

- **Event format:** the admin chooses **Classic flow** or **Game Show** before the first Participant registers (or after a Reset); it can't change mid-event. Both formats share registration, Personas, matching, Themes and the Reveal.
- **Host:** the admin runs the show from a phone-friendly **host console**, reachable from the admin page. Each Round has a countdown (default 15s, settable per Round), and the Host decides when to move on.
- **Questions:** 11 for everyone, in both formats: the 6 Fixed Questions plus 5 personalised ones.
  - GitHub users get Custom Questions, topped up with Extra Questions if the LLM returns fewer than 5 or fails.
  - Non-GitHub Users get the 5 Extra Questions.
  - Nobody skips "go-to language" any more.
- **Game Show questions:** the personalised questions are answered individually in the **Lobby**. The Fixed Questions become the six **Rounds**.
  - Answers can change until a Round closes.
  - A missed Round is no answer.
  - Round answers are stored like Interview answers.
- **Personas** are created right after the Lobby questions, so the stage shows Personas from Round 1.
- **Matching in a Game Show:**
  - No Continuous Matching before the matchmaking segment.
  - The LLM assesses Pairs in the background, throttled, as Participants finish the Lobby (profile plus personalised answers).
  - Round answers feed only the heuristic score.
  - The matchmaking segment pairs everyone ready, instantly, from the cached assessments.
- **Late arrivals** join the Lobby and the remaining Rounds, and are paired at the matchmaking segment if they're ready. After the Reveal, the **Afterparty** uses Continuous Matching, and arrivals see their Match at once.
- **Show segments:** Lobby, Rounds 1 to 6 (each open, then closed), Matchmaking, the Reveal (pair by pair), Afterparty. The current segment is stored with the Event State, so a restart resumes the show.
- **The stage during a Round:** Persona symbols drop into live answer columns. At close there's a callout with the majority and a minority "hot take".
- **The phone as controller:**
  - Two-option Rounds are swipe cards; 3–4 option Rounds use colour answer pads.
  - Lobby questions are form cards.
  - Between Rounds the phone shows the full Persona colour, "waiting for the Host" and status lines.
- **The Reveal:** pair by pair and automatic, and the Host can skip.
  - A drumroll, and the pair's Personas light up on stage.
  - Their phones switch to the Match's colour with "Stand up!" and the full-screen "It's a Match!" pop.
  - An odd Participant out gets a wildcard moment and is paired by Continuous Matching with the next arrival.
- **Themes:** five show moments are added to the token set's eight: a Round opens, a Round closes (with the callout), the countdown's last 3 seconds, the matchmaking drumroll, and a pair revealed. Any of them may be silent.
- **Modules:**
  - A new **Show module** owns the show segments, Rounds, countdowns and Round answers, on top of the Event State and the change feed.
  - Reused: the Interview module (the Lobby), Onboarding (Personas), Matchmaking (background assessment and pairing everyone at the segment) and the Relationship module.
  - Interfaces are settled at build time with the codebase-design skill.
- **Stack:** Go templates, HTMX and SSE; the self-hosted assets from ADR-0009; small vanilla JavaScript for swipe cards and countdowns. No front-end framework.

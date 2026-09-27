package main

import "testing"

func TestDestination_EveryParticipantHasOnePlaceToBe(t *testing.T) {
	const onboard, wait, match = "/user/onboard/p", "/user/wait/p", "/user/match/p"
	questions := []Question{{ID: "q1"}, {ID: "q2"}}
	// Expected destination per column: unmatched, unmatched+revealed, matched, matched+revealed.
	for _, row := range []struct {
		name    string
		step    Step
		answers map[string]string
		want    [4]string
	}{
		{"fetching GitHub", StepFetchingGitHub, nil, [4]string{onboard, onboard, onboard, onboard}},
		{"interviewing, open questions", StepInterviewing, map[string]string{"q1": "x"}, [4]string{onboard, onboard, onboard, onboard}},
		{"interviewing, all answered", StepInterviewing, map[string]string{"q1": "x", "q2": "y"}, [4]string{wait, wait, wait, wait}},
		{"creating persona", StepCreatingPersona, nil, [4]string{wait, wait, wait, wait}},
		{"ready", StepReady, nil, [4]string{wait, wait, wait, match}},
	} {
		for col, c := range []struct{ matched, revealed bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
			p := &Participant{ID: "p", PipelineStep: row.step, Questions: questions, Answers: row.answers}
			if c.matched {
				p.MatchedWith = "q"
			}
			if got := destination(p, c.revealed, ClassicFlow); got != row.want[col] {
				t.Errorf("%s (matched %v, revealed %v): want %s, got %s", row.name, c.matched, c.revealed, row.want[col], got)
			}
		}
	}
}

// TestDestination_GameShowSendsAReadyParticipantToTheShowStatusPage covers
// #78: before the Reveal, a ready Participant in a Game Show belongs on the
// show status page, not the Classic flow's wait page; after the Reveal
// they're matched like anyone else.
func TestDestination_GameShowSendsAReadyParticipantToTheShowStatusPage(t *testing.T) {
	p := &Participant{ID: "p", PipelineStep: StepReady}

	if got := destination(p, false, GameShow); got != "/user/show/p" {
		t.Errorf("want the show status page, got %s", got)
	}

	p.MatchedWith = "q"
	if got := destination(p, true, GameShow); got != "/user/match/p" {
		t.Errorf("once revealed and matched, want the match page, got %s", got)
	}
}

// TestDestination_GameShowWildcardStaysOnTheShowStatusPageOnceRevealed
// covers #83: the wildcard is never matched, so without this they would
// otherwise fall through to the wait page once revealed is true, missing
// their own Reveal message.
func TestDestination_GameShowWildcardStaysOnTheShowStatusPageOnceRevealed(t *testing.T) {
	p := &Participant{ID: "p", PipelineStep: StepReady, Wildcard: true}

	if got := destination(p, true, GameShow); got != "/user/show/p" {
		t.Errorf("want the wildcard kept on the show status page, got %s", got)
	}
}

func TestSubmitAnswer_WhileMatchedGoesToTheRightPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.Reveal()
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")

	resp := post(t, srv, "/user/answer/a", nil)

	if loc := resp.Header.Get("HX-Redirect"); loc != "/user/match/a" {
		t.Errorf("a matched Participant after the Reveal belongs on their match page, got %q", loc)
	}
}

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
			if got := destination(p, c.revealed); got != row.want[col] {
				t.Errorf("%s (matched %v, revealed %v): want %s, got %s", row.name, c.matched, c.revealed, row.want[col], got)
			}
		}
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

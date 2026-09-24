package main

import "testing"

func TestDestination_EveryParticipantHasOnePlaceToBe(t *testing.T) {
	questions := []Question{{ID: "q1"}, {ID: "q2"}}
	for name, tc := range map[string]struct {
		step     Step
		answers  map[string]string
		matched  bool
		revealed bool
		want     string
	}{
		"fetching GitHub":              {step: "fetching_github", want: "/user/onboard/p"},
		"interviewing, open questions": {step: "interviewing", answers: map[string]string{"q1": "x"}, want: "/user/onboard/p"},
		"interviewing, all answered":   {step: "interviewing", answers: map[string]string{"q1": "x", "q2": "y"}, want: "/user/wait/p"},
		"creating persona":             {step: "creating_persona", want: "/user/wait/p"},
		"ready, unmatched":             {step: "ready", want: "/user/wait/p"},
		"ready, unmatched, revealed":   {step: "ready", revealed: true, want: "/user/wait/p"},
		"matched before the Reveal":    {step: "ready", matched: true, want: "/user/wait/p"},
		"matched after the Reveal":     {step: "ready", matched: true, revealed: true, want: "/user/match/p"},
	} {
		t.Run(name, func(t *testing.T) {
			p := &Participant{ID: "p", PipelineStep: tc.step, Questions: questions, Answers: tc.answers}
			if tc.matched {
				p.MatchedWith = "q"
			}

			if got := destination(p, tc.revealed); got != tc.want {
				t.Errorf("want %s, got %s", tc.want, got)
			}
		})
	}
}

func TestSubmitAnswer_WhileMatchedGoesToTheRightPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	deps.db.SetPhase("revealed")
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")

	resp := post(t, srv, "/user/answer/a", nil)

	if loc := resp.Header.Get("HX-Redirect"); loc != "/user/match/a" {
		t.Errorf("a matched Participant after the Reveal belongs on their match page, got %q", loc)
	}
}

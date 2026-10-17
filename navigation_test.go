package main

import "testing"

func TestDestination_EveryParticipantHasOnePlaceToBe(t *testing.T) {
	const onboard, wait, match = "/user/onboard/p", "/user/wait/p", "/user/match/p"
	questions := []Question{{ID: "q1"}, {ID: "q2"}}
	// Expected destination per row, in every phase: onboarding, social, showtell, closed.
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
		{"ready", StepReady, nil, [4]string{wait, wait, wait, wait}},
	} {
		for col, phase := range phases {
			p := &Participant{ID: "p", PipelineStep: row.step, Questions: questions, Answers: row.answers}
			if got := destination(p, phase); got != row.want[col] {
				t.Errorf("%s (%s): want %s, got %s", row.name, phase, row.want[col], got)
			}
		}
	}
	// A matched Participant waits until the Social begins, then belongs on
	// their match page — in the Social and in every later phase.
	for _, phase := range phases {
		p := &Participant{ID: "p", PipelineStep: StepReady, Questions: questions,
			Answers: map[string]string{"q1": "x", "q2": "y"}, MatchedWith: "q"}
		want := wait
		if phase.AtLeast(PhaseSocial) {
			want = match
		}
		if got := destination(p, phase); got != want {
			t.Errorf("matched (%s): want %s, got %s", phase, want, got)
		}
	}
}

func TestSubmitAnswer_WhileMatchedGoesToTheRightPage(t *testing.T) {
	srv, deps := newTestServer(t, nil, nil)
	must(t, deps.events.Reveal())
	seed(t, deps.db, "a", "A", "ready")
	seed(t, deps.db, "b", "B", "ready")
	pair(t, deps.db, "a", "b")

	resp := post(t, srv, "/user/answer/a", nil)

	if loc := resp.Header.Get("HX-Redirect"); loc != "/user/match/a" {
		t.Errorf("a matched Participant after the Reveal belongs on their match page, got %q", loc)
	}
}

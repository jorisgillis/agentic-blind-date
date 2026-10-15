package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestHeuristicScore_MapsThePairScoreOntoTheCompatibilityScale covers the
// fallback score's contract: the PairScore heuristic mapped onto 0-100, with
// a floor for nothing shared and a cap for everything shared, monotonic in
// between — the fallback discriminates between pairs instead of presenting
// one constant (the field report's "everybody had a 42% match").
func TestHeuristicScore_MapsThePairScoreOntoTheCompatibilityScale(t *testing.T) {
	if got := heuristicScore(0); got != 30 {
		t.Errorf("nothing shared: want the scale floor 30, got %d", got)
	}
	if got := heuristicScore(35); got != 100 {
		t.Errorf("35 heuristic points: want the scale cap 100, got %d", got)
	}
	if got := heuristicScore(1000); got != 100 {
		t.Errorf("the scale caps at 100, got %d", got)
	}
	prev := heuristicScore(0)
	for raw := 1; raw <= 40; raw++ {
		if got := heuristicScore(raw); got < prev {
			t.Fatalf("heuristicScore(%d) = %d, below heuristicScore(%d) = %d", raw, got, raw-1, prev)
		} else {
			prev = got
		}
	}
}

// TestMatcherHeuristicFallback_DiscriminatesAndSaysSo covers the fallback
// assessment used when the LLM scoring fails: the score comes from the
// heuristic (pairs sharing more score higher), and the reason says honestly
// that it is an estimate — while the flags and icebreakers keep the dossier
// payload complete.
func TestMatcherHeuristicFallback_DiscriminatesAndSaysSo(t *testing.T) {
	m := NewMatcher(newTestDB(t), newFakeGitHub(), newFakeLLM())

	strangers := m.heuristicMatchResult(testParticipant("a", "A", []string{"Go"}), testParticipant("b", "B", []string{"Rust"}))
	acquaintances := m.heuristicMatchResult(
		testParticipant("c", "C", []string{"Go"}, participantOpts{answers: map[string]string{"fixed_0": "Tabs"}}),
		testParticipant("d", "D", []string{"Go", "Rust"}, participantOpts{answers: map[string]string{"fixed_0": "Tabs"}}),
	)

	if strangers.Score != heuristicScore(0) {
		t.Errorf("nothing shared: want the floor %d, got %d", heuristicScore(0), strangers.Score)
	}
	if acquaintances.Score != heuristicScore(4) {
		t.Errorf("one shared language and one shared answer: want %d, got %d", heuristicScore(4), acquaintances.Score)
	}
	if acquaintances.Score <= strangers.Score {
		t.Errorf("sharing more must score higher: got %d vs %d", acquaintances.Score, strangers.Score)
	}
	if !strings.Contains(strangers.Reason, "Estimated") {
		t.Errorf("the reason should say the score is an estimate, got %q", strangers.Reason)
	}
	if len(strangers.RedFlags) != 0 || len(strangers.GreenFlags) != 1 || len(strangers.Icebreakers) != 3 {
		t.Errorf("the fallback keeps the payload complete, got %+v", strangers)
	}
}

const matchReply = `{"score": 87, "reason": "Both refuse to use tabs", "red_flags": ["argues about vim, emacs, and nano"], "green_flags": [], "icebreakers": ["Why Go?", "Monorepo, yes or no?"]}`

// participantOpts customizes testParticipant beyond ID, persona and languages.
type participantOpts struct {
	topics      []string
	projectType string
	devEnv      []string
	answers     map[string]string
}

// testParticipant builds a bare, in-memory Participant for Matcher unit
// tests (PairScore, ScorePair), which never touch a database. langs sets
// both the GitHub profile's languages and Interests.
func testParticipant(id, persona string, langs []string, opts ...participantOpts) *Participant {
	var o participantOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	profile := &GitHubProfile{Login: id, Languages: langs, TopTopics: o.topics}
	if o.projectType != "" || len(o.devEnv) > 0 {
		profile.ExtraAnswers = &ExtraAnswers{ProjectType: o.projectType, DevEnvironment: o.devEnv}
	}
	return &Participant{
		ID: id, GitHubHandle: id, HasGitHub: true, PersonaName: persona,
		Profile: profile, Answers: o.answers, Interests: Interests{Languages: langs},
	}
}

func TestMatcherScorePair_ScoresAPairOnceInEitherOrder(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	a, b := testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"})

	first, err := m.ScorePair(a, b)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.ScorePair(b, a)
	if err != nil {
		t.Fatal(err)
	}

	if n := llm.callsMatching("matchmaker"); n != 1 {
		t.Errorf("LLM calls: want 1, got %d", n)
	}
	if first.Score != 87 || second.Score != 87 {
		t.Errorf("scores: got %d and %d", first.Score, second.Score)
	}
}

func TestMatcherScorePair_CachedResultSurvivesARestartExactly(t *testing.T) {
	db := newTestDB(t)
	llm := newFakeLLM().on("matchmaker", matchReply)
	a, b := testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"})
	if _, err := NewMatcher(db, newFakeGitHub(), llm).ScorePair(a, b); err != nil {
		t.Fatal(err)
	}

	restarted := NewMatcher(db, newFakeGitHub(), llm)
	got, err := restarted.ScorePair(a, b)
	if err != nil {
		t.Fatal(err)
	}

	if n := llm.callsMatching("matchmaker"); n != 1 {
		t.Errorf("a restarted Matcher should reuse the persistent cache; LLM calls: %d", n)
	}
	if len(got.RedFlags) != 1 || got.RedFlags[0] != "argues about vim, emacs, and nano" {
		t.Errorf("red flags must round-trip exactly, got %q", got.RedFlags)
	}
	if got.GreenFlags == nil || len(got.GreenFlags) != 0 {
		t.Errorf("empty green flags must stay empty, got %q", got.GreenFlags)
	}
	if len(got.Icebreakers) != 2 || got.Icebreakers[1] != "Monorepo, yes or no?" {
		t.Errorf("icebreakers: got %q", got.Icebreakers)
	}
}

func TestMatcherScorePair_FailuresAreNotCached(t *testing.T) {
	llm := newFakeLLM().onErr("matchmaker", fakeError("mistral HTTP 429"))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	a, b := testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"})

	if _, err := m.ScorePair(a, b); err == nil {
		t.Fatal("want error")
	}
	m.ScorePair(a, b)

	if n := llm.callsMatching("matchmaker"); n != 2 {
		t.Errorf("a failed scoring must be retried next time; LLM calls: %d", n)
	}
}

func TestMatcherScorePair_PromptShowsAnswersWithTheirQuestions(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	a := testParticipant("a", "The Gopher", []string{"Go"}, participantOpts{answers: map[string]string{"fixed_0": "Tabs"}})
	a.Questions = []Question{{ID: "fixed_0", Text: "Tabs or spaces?"}}

	m.ScorePair(a, testParticipant("b", "The Crab", []string{"Rust"}))

	call, _ := llm.lastCallMatching("matchmaker")
	if !strings.Contains(call.User, "- Tabs or spaces? → Tabs") {
		t.Errorf("prompt should pair the answer with its question:\n%s", call.User)
	}
	if !strings.Contains(call.User, "Interview answers:\n(none)") {
		t.Errorf("a Developer without answers should say so:\n%s", call.User)
	}
}

func TestMatcherScorePair_PromptDescribesEachDeveloperWithTheirOwnInterestsAndFollows(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	gh := newFakeGitHub().withFollow("a", "b")
	m := NewMatcher(newTestDB(t), gh, llm)

	m.ScorePair(testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"}))

	call, _ := llm.lastCallMatching("matchmaker")
	dev1, dev2, found := strings.Cut(call.User, "DEVELOPER 2")
	if !found {
		t.Fatalf("prompt has no DEVELOPER 2 section:\n%s", call.User)
	}
	if !strings.Contains(dev1, "Interests: languages: Go") || strings.Contains(dev1, "Rust") {
		t.Errorf("DEVELOPER 1 section should hold only their own interests:\n%s", dev1)
	}
	if !strings.Contains(dev2, "Interests: languages: Rust") {
		t.Errorf("DEVELOPER 2 section should hold their own interests:\n%s", dev2)
	}
	if !strings.Contains(call.User, "The Gopher already follows The Crab on GitHub.") {
		t.Errorf("prompt should mention the follow relationship:\n%s", call.User)
	}
}

// TestMatcherScorePair_AHungFollowCheckStillCompletesTheAssessment covers
// #46: a follow check that never gets a response must not stall a Match
// assessment, and a timed-out check is treated as no follow relationship.
func TestMatcherScorePair_AHungFollowCheckStillCompletesTheAssessment(t *testing.T) {
	gh := NewGitHubClient("secret")
	gh.httpClient = &http.Client{Transport: hangingTransport{}, Timeout: 20 * time.Millisecond}
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), gh, llm)

	start := time.Now()
	result, err := m.ScorePair(testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"}))

	if err != nil {
		t.Fatalf("assessment should still complete, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("assessment should be bounded by the GitHub timeout, took %v", elapsed)
	}
	if result.Score == 0 {
		t.Errorf("want a real assessment, got %+v", result)
	}
	call, _ := llm.lastCallMatching("matchmaker")
	if strings.Contains(call.User, "follow") {
		t.Errorf("a hung follow check should add no follow note to the prompt:\n%s", call.User)
	}
}

func TestMatcherScorePair_ClampsOutOfRangeScores(t *testing.T) {
	for reply, want := range map[string]int{
		`{"score": 140, "reason": "Too good"}`: 100,
		`{"score": -5, "reason": "Oof"}`:       0,
	} {
		llm := newFakeLLM().on("matchmaker", reply)
		m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

		got, err := m.ScorePair(testParticipant("a", "A", nil), testParticipant("b", "B", nil))

		if err != nil || got.Score != want {
			t.Errorf("%s: want score %d, got %+v (err %v)", reply, want, got, err)
		}
	}
}

func TestMatcherScorePair_RejectsRepliesWithoutAScore(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", `{"reason": "forgot the number"}`)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	if _, err := m.ScorePair(testParticipant("a", "A", nil), testParticipant("b", "B", nil)); err == nil {
		t.Error("a reply without a score should be an error")
	}
}

// scoreTable scripts the fake LLM to score Pairs by persona name, in either order.
func scoreTable(scores map[[2]string]int) func(user string) (string, error) {
	return func(user string) (string, error) {
		name := func(n string) string {
			rest := user[strings.Index(user, "DEVELOPER "+n+" (")+len("DEVELOPER "+n+" ("):]
			return rest[:strings.Index(rest, ")")]
		}
		a, b := name("1"), name("2")
		s, ok := scores[[2]string{a, b}]
		if !ok {
			s, ok = scores[[2]string{b, a}]
		}
		if !ok {
			s = 50
		}
		return fmt.Sprintf(`{"score": %d, "reason": "%s and %s"}`, s, a, b), nil
	}
}

func pairsOf(matches []Match) []string {
	var out []string
	for _, m := range matches {
		a, b := m.A.PersonaName, m.B.PersonaName
		if b < a {
			a, b = b, a
		}
		out = append(out, fmt.Sprintf("%s-%s:%d", a, b, m.Result.Score))
	}
	sort.Strings(out)
	return out
}

func readyDev(id string) *Participant {
	p := testParticipant(id, id, []string{"Go"})
	p.PipelineStep = "ready"
	return p
}

func TestMatcherMatchPool_AssignsGreedilyByLLMScore(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{
		{"A", "C"}: 95, {"A", "B"}: 90, {"C", "D"}: 80, {"B", "D"}: 10,
	}))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	matches := m.MatchPool([]*Participant{readyDev("A"), readyDev("B"), readyDev("C"), readyDev("D")})

	if got, want := strings.Join(pairsOf(matches), " "), "A-C:95 B-D:10"; got != want {
		t.Errorf("matches: want %s, got %s", want, got)
	}
}

func TestMatcherMatchPool_EachParticipantIsInAtMostOneMatch(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(nil))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	pool := []*Participant{readyDev("A"), readyDev("B"), readyDev("C"), readyDev("D"), readyDev("E")}

	matches := m.MatchPool(pool)

	if len(matches) != 2 {
		t.Fatalf("5 Participants make 2 Matches, got %v", pairsOf(matches))
	}
	seen := map[string]bool{}
	for _, match := range matches {
		for _, p := range []*Participant{match.A, match.B} {
			if seen[p.ID] {
				t.Fatalf("%s is in more than one Match: %v", p.ID, pairsOf(matches))
			}
			seen[p.ID] = true
		}
	}
}

func TestMatcherMatchPool_UsesTheHeuristicFallbackWhenScoringFails(t *testing.T) {
	llm := newFakeLLM().onErr("matchmaker", fakeError("mistral HTTP 500"))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	matches := m.MatchPool([]*Participant{readyDev("A"), readyDev("B")})

	// Both share Go (+3 heuristic points): the fallback scores the pair from
	// the heuristic, not a constant.
	want := heuristicScore(3)
	if len(matches) != 1 || matches[0].Result.Score != want {
		t.Errorf("want one Match with the heuristic fallback score %d, got %v", want, pairsOf(matches))
	}
}

func TestMatcherMatchPool_NeedsTwoParticipants(t *testing.T) {
	m := NewMatcher(newTestDB(t), newFakeGitHub(), newFakeLLM())

	if matches := m.MatchPool([]*Participant{readyDev("A")}); len(matches) != 0 {
		t.Errorf("a lone Participant cannot be matched, got %v", pairsOf(matches))
	}
}

func matchedDev(id, partner string, score int) *Participant {
	p := testParticipant(id, id, []string{"Go"})
	p.PipelineStep, p.MatchedWith = "matched", partner
	p.Assessment = &matchResult{Score: score}
	return p
}

func TestMatcherMatchNewcomer_PairsWithTheBestScoringUnmatchedParticipant(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{{"N", "A"}: 30, {"N", "B"}: 80, {"N", "C"}: 99}))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	match := m.MatchNewcomer(readyDev("N"), []*Participant{readyDev("A"), readyDev("B"), matchedDev("C", "D", 10)})

	if match == nil || match.B.ID != "B" || match.Result.Score != 80 {
		t.Fatalf("want N-B:80 (unmatched Participants come first), got %v", match)
	}
}

func TestMatcherMatchNewcomer_TakesOverAMatchItBeats(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{
		{"N", "A"}: 70, {"N", "B"}: 20, {"N", "C"}: 60, {"N", "D"}: 10,
	}))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	pool := []*Participant{matchedDev("A", "B", 40), matchedDev("B", "A", 40), matchedDev("C", "D", 90), matchedDev("D", "C", 90)}

	match := m.MatchNewcomer(readyDev("N"), pool)

	if match == nil || match.B.ID != "A" || match.Result.Score != 70 {
		t.Fatalf("want N-A:70 (beats A's current 40; C's 90 is not beaten), got %v", match)
	}
}

func TestMatcherMatchNewcomer_StaysUnmatchedWhenItBeatsNoMatch(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(map[[2]string]int{{"N", "A"}: 30, {"N", "B"}: 30}))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)

	match := m.MatchNewcomer(readyDev("N"), []*Participant{matchedDev("A", "B", 90), matchedDev("B", "A", 90)})

	if match != nil {
		t.Errorf("want no Match, got %s-%s", match.A.ID, match.B.ID)
	}
}

func TestMatcherMatchNewcomer_WithAnEmptyPool(t *testing.T) {
	m := NewMatcher(newTestDB(t), newFakeGitHub(), newFakeLLM())

	if match := m.MatchNewcomer(readyDev("N"), nil); match != nil {
		t.Errorf("want no Match, got %v", match)
	}
}

func TestMatcherMatchPool_OnlyAssessesHeuristicTopCandidates(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(nil))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	// A and B share nothing, while both share a language with everyone else, so each
	// ranks the other last among six others and neither makes the other's top 5.
	pool := []*Participant{
		ready(testParticipant("A", "A", []string{"Go"})), ready(testParticipant("B", "B", []string{"Rust"})),
		ready(testParticipant("C", "C", []string{"Go", "Rust"})), ready(testParticipant("D", "D", []string{"Go", "Rust"})), ready(testParticipant("E", "E", []string{"Go", "Rust"})),
		ready(testParticipant("F", "F", []string{"Go", "Rust"})), ready(testParticipant("G", "G", []string{"Go", "Rust"})),
	}

	m.MatchPool(pool)

	if llm.callsMatching("matchmaker") == 0 {
		t.Fatal("no Pair was assessed at all")
	}
	for _, c := range llm.calls {
		if strings.Contains(c.User, "DEVELOPER 1 (A)") && strings.Contains(c.User, "DEVELOPER 2 (B)") ||
			strings.Contains(c.User, "DEVELOPER 1 (B)") && strings.Contains(c.User, "DEVELOPER 2 (A)") {
			t.Error("A-B is outside both top-5 lists but was assessed by the LLM")
		}
	}
}

func ready(p *Participant) *Participant {
	p.PipelineStep = "ready"
	return p
}

func TestMatcherScorePair_PromptMentionsMutualAndReverseFollows(t *testing.T) {
	for name, tc := range map[string]struct {
		gh   *fakeGitHub
		want string
	}{
		"mutual":  {newFakeGitHub().withFollow("a", "b").withFollow("b", "a"), "The Gopher and The Crab already follow each other on GitHub!"},
		"reverse": {newFakeGitHub().withFollow("b", "a"), "The Crab already follows The Gopher on GitHub."},
		"none":    {newFakeGitHub(), ""},
	} {
		t.Run(name, func(t *testing.T) {
			llm := newFakeLLM().on("matchmaker", matchReply)
			m := NewMatcher(newTestDB(t), tc.gh, llm)

			m.ScorePair(testParticipant("a", "The Gopher", []string{"Go"}), testParticipant("b", "The Crab", []string{"Rust"}))

			call, _ := llm.lastCallMatching("matchmaker")
			if tc.want == "" && strings.Contains(call.User, "follow") {
				t.Errorf("no follows: prompt should not mention following:\n%s", call.User)
			}
			if tc.want != "" && !strings.Contains(call.User, tc.want) {
				t.Errorf("prompt should contain %q:\n%s", tc.want, call.User)
			}
		})
	}
}

func TestMatcherMatchPool_PairsParticipantsLeftOutOfEveryCandidatePair(t *testing.T) {
	scores := map[[2]string]int{}
	others := []string{"C", "D", "E", "F", "G", "H"}
	for i, a := range others {
		for _, b := range others[i+1:] {
			scores[[2]string{a, b}] = 90
		}
		scores[[2]string{"A", a}], scores[[2]string{"B", a}] = 10, 10
	}
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(scores))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	// A and B are outside each other's top 5, and everyone they are a candidate with
	// is taken by a 90, so they are only paired by the leftover heuristic pass.
	pool := []*Participant{ready(testParticipant("A", "A", []string{"Go"})), ready(testParticipant("B", "B", []string{"Rust"}))}
	for _, id := range others {
		pool = append(pool, ready(testParticipant(id, id, []string{"Go", "Rust"})))
	}

	matches := m.MatchPool(pool)

	got := strings.Join(pairsOf(matches), " ")
	if len(matches) != 4 || !strings.Contains(got, "A-B:50") {
		t.Errorf("want all 8 matched, including leftovers A-B, got %s", got)
	}
}

func TestMatcherScorePair_NoFollowLookupsForNonGitHubUsers(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	gh := newFakeGitHub()
	m := NewMatcher(newTestDB(t), gh, llm)
	ada := testParticipant("no-github-1234", "The Ada", nil)
	ada.HasGitHub = false

	m.ScorePair(testParticipant("a", "The Gopher", []string{"Go"}), ada)

	if n := gh.followLookups(); n != 0 {
		t.Errorf("follow lookups for a Non-GitHub User: want 0, got %d", n)
	}
}

// TestMatcher_FollowChecksAreCachedPerPairAcrossScoringAndFollowNotes is #112:
// the counting GitHub fake proves one follow check per pair per event, in
// either order, shared by the heuristic PairScore and the LLM follow notes.
func TestMatcher_FollowChecksAreCachedPerPairAcrossScoringAndFollowNotes(t *testing.T) {
	gh := newFakeGitHub().withFollow("a", "b")
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), gh, llm)
	a := testParticipant("a", "The Gopher", []string{"Go"})
	b := testParticipant("b", "The Crab", []string{"Rust"})

	// A one-way follow, scored repeatedly in both orders: one check per pair.
	for _, score := range []int{m.PairScore(b, a), m.PairScore(a, b), m.PairScore(b, a)} {
		if score != 3 {
			t.Errorf("PairScore with a one-way follow: want 3, got %d", score)
		}
	}
	if n := gh.followLookups(); n != 1 {
		t.Fatalf("repeated PairScore in either order: want 1 follow lookup per pair, got %d", n)
	}

	// The LLM follow notes reuse the same check.
	if _, err := m.ScorePair(a, b); err != nil {
		t.Fatal(err)
	}
	call, _ := llm.lastCallMatching("matchmaker")
	if !strings.Contains(call.User, "The Gopher already follows The Crab on GitHub.") {
		t.Errorf("follow note missing from the prompt:\n%s", call.User)
	}
	if n := gh.followLookups(); n != 1 {
		t.Errorf("PairScore and the follow notes should share one lookup per pair, got %d", n)
	}
}

func TestMatcherScorePair_AnUnreadableReplyIsAnError(t *testing.T) {
	m := NewMatcher(newTestDB(t), newFakeGitHub(), newFakeLLM().on("matchmaker", "I refuse to score humans"))

	if _, err := m.ScorePair(testParticipant("a", "A", nil), testParticipant("b", "B", nil)); err == nil {
		t.Error("want an error")
	}
}

func TestMatcher_ParticipantsWithoutProfileOrAnswers(t *testing.T) {
	llm := newFakeLLM().on("matchmaker", matchReply)
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	bare := &Participant{ID: "bare", PersonaName: "The Blank"}

	if _, err := m.ScorePair(testParticipant("a", "A", []string{"Go"}), bare); err != nil {
		t.Fatal(err)
	}
	if got := m.PairScore(bare, testParticipant("a", "A", []string{"Go"})); got != 0 {
		t.Errorf("no profile scores 0, got %d", got)
	}
	if got := m.PairScore(testParticipant("a", "A", []string{"Go"}), bare); got != 0 {
		t.Errorf("no profile scores 0, got %d", got)
	}
}

func TestMatcher_TheNewcomerIsNotTheirOwnCandidateAndOnlyReadyParticipantsArePaired(t *testing.T) {
	llm := newFakeLLM().onFunc("matchmaker", scoreTable(nil))
	m := NewMatcher(newTestDB(t), newFakeGitHub(), llm)
	n := readyDev("N")

	if match := m.MatchNewcomer(n, []*Participant{n, readyDev("A")}); match == nil || match.B.ID != "A" {
		t.Errorf("want N-A, got %+v", match)
	}
	busy := testParticipant("I", "I", []string{"Go"})
	busy.PipelineStep = StepInterviewing
	for _, match := range m.MatchPool([]*Participant{readyDev("A"), readyDev("B"), busy}) {
		if match.A.ID == "I" || match.B.ID == "I" {
			t.Error("a Participant still interviewing must not be paired")
		}
	}
}

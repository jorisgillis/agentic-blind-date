package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// matchResult contains the LLM-generated compatibility assessment between two participants.
type matchResult struct {
	Score       int      `json:"score"`
	Reason      string   `json:"reason"`
	RedFlags    []string `json:"red_flags"`
	GreenFlags  []string `json:"green_flags"`
	Icebreakers []string `json:"icebreakers"`
}

// Matcher pairs Participants. It owns the heuristic Pair score, the LLM
// compatibility assessment (prompt, parsing, validation), the two-level cache
// of assessments from ADR-0002 and the in-memory cache of GitHub follow
// relationships for the duration of the event.
type Matcher struct {
	db     *DB
	github GitHubAPI
	llm    LLM

	mu    sync.Mutex
	cache map[string]*matchResult // in-memory level; the llm_cache table is the persistent level

	followMu sync.Mutex
	follows  map[string]followCheck // in-memory follow relationships, keyed by unordered handle pair
}

// NewMatcher creates a new Matcher instance.
func NewMatcher(db *DB, github GitHubAPI, llm LLM) *Matcher {
	return &Matcher{
		db:      db,
		github:  github,
		llm:     llm,
		cache:   make(map[string]*matchResult),
		follows: make(map[string]followCheck),
	}
}

// heuristicMatchResult is the fallback assessment when the LLM scoring
// fails — a rate-limited provider under a rush of registrations. The score is
// the PairScore heuristic mapped onto the 0-100 scale, so the fallback still
// discriminates between pairs instead of presenting one constant, and the
// reason says honestly that the score is an estimate. The generic flags and
// icebreakers keep the dossier payload complete.
func (m *Matcher) heuristicMatchResult(a, b *Participant) *matchResult {
	return &matchResult{
		Score:       heuristicScore(m.PairScore(a, b)),
		Reason:      "Estimated from your shared languages, tools and answers — the matchmaker agent is catching up.",
		RedFlags:    []string{},
		GreenFlags:  []string{"You're both here tonight"},
		Icebreakers: []string{"What brings you to this meetup?", "What are you currently building?", "Best tech talk you've seen recently?"},
	}
}

// heuristicScore maps the PairScore heuristic onto the compatibility scale:
// a Pair with nothing in common lands at 30, every shared language, topic,
// answer, environment or follow adds 2, and the scale caps at 100.
func heuristicScore(raw int) int {
	return min(100, 30+raw*2)
}

// ScorePair returns the LLM compatibility assessment for a Pair, in either
// order. Assessments are cached in memory and in the database; failures are
// not cached, so the next call retries.
func (m *Matcher) ScorePair(a, b *Participant) (*matchResult, error) {
	key := pairKey(a, b)

	m.mu.Lock()
	cached, ok := m.cache[key]
	m.mu.Unlock()
	if ok {
		logger("matcher").Debug("assessment cache hit", "pair", key)
		return cached, nil
	}
	if cached, ok := m.db.GetLLMCache(key); ok {
		logger("matcher").Debug("assessment cache hit, persistent", "pair", key)
		m.remember(key, cached)
		return cached, nil
	}

	result, err := m.assess(a, b)
	if err != nil {
		return nil, err
	}
	m.remember(key, result)
	m.db.SetLLMCache(key, result)
	return result, nil
}

// ClearCache forgets every cached assessment, in memory and in the database,
// and every cached follow relationship, in memory.
func (m *Matcher) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]*matchResult)
	m.mu.Unlock()
	m.followMu.Lock()
	m.follows = make(map[string]followCheck)
	m.followMu.Unlock()
	m.db.ClearLLMCache()
}

func (m *Matcher) remember(key string, result *matchResult) {
	m.mu.Lock()
	m.cache[key] = result
	m.mu.Unlock()
}

func pairKey(a, b *Participant) string {
	if a.ID < b.ID {
		return a.ID + ":" + b.ID
	}
	return b.ID + ":" + a.ID
}

// matchAssessmentReply is the LLM's raw reply shape for a Match assessment.
// Score is a pointer so a missing field is distinguishable from a zero score.
type matchAssessmentReply struct {
	Score       *int     `json:"score"`
	Reason      string   `json:"reason"`
	RedFlags    []string `json:"red_flags"`
	GreenFlags  []string `json:"green_flags"`
	Icebreakers []string `json:"icebreakers"`
}

func (m *Matcher) assess(p1, p2 *Participant) (*matchResult, error) {
	system := `You are the matchmaker at a tech meetup blind date event.
Analyze two developers' profiles and produce a fun, humorous compatibility assessment.
Respond with ONLY valid JSON — no markdown:
{"score": <0-100>, "reason": "<one funny sentence max 80 chars>", "red_flags": ["...", "..."], "green_flags": ["...", "..."], "icebreakers": ["<question one can ask the other>", "<question>", "<question>"]}`

	user := "Compare these two developers:\n\n" +
		describeDeveloper(1, p1) + "\n\n" + describeDeveloper(2, p2) + m.followNote(p1, p2)

	reply, err := AskStructured(m.llm, system, user, func(r matchAssessmentReply) error {
		if r.Score == nil {
			return errors.New("match reply has no score")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &matchResult{
		Score:       min(max(*reply.Score, 0), 100),
		Reason:      reply.Reason,
		RedFlags:    nonNil(reply.RedFlags),
		GreenFlags:  nonNil(reply.GreenFlags),
		Icebreakers: nonNil(reply.Icebreakers),
	}, nil
}

func describeDeveloper(n int, p *Participant) string {
	s := fmt.Sprintf("DEVELOPER %d (%s):\n%s\nInterview answers:\n%s", n, p.PersonaName, p.Summary(), interviewText(p))
	if interests := fmtInterests(p.Interests); interests != "" {
		s += "\nInterests: " + interests
	}
	return s
}

// followCheck is a cached follow relationship between two handles, stored in
// the cache key's order (first < second).
type followCheck struct {
	firstFollowsSecond bool
	secondFollowsFirst bool
}

// orderedPair orders two handles like the follow cache key: the
// lexicographically lower first.
func orderedPair(a, b string) (first, second string) {
	first, second = a, b
	if second < first {
		first, second = second, first
	}
	return first, second
}

// directed reads the cached follow relationship from the querying side: the
// cache stores it in the key's order, so the answer flips with the query.
func (c followCheck) directed(firstIsA bool) (aFollowsB, bFollowsA bool) {
	if firstIsA {
		return c.firstFollowsSecond, c.secondFollowsFirst
	}
	return c.secondFollowsFirst, c.firstFollowsSecond
}

// newFollowCheck stores a follow relationship in the cache key's order.
func newFollowCheck(firstIsA bool, aFollowsB, bFollowsA bool) followCheck {
	if firstIsA {
		return followCheck{firstFollowsSecond: aFollowsB, secondFollowsFirst: bFollowsA}
	}
	return followCheck{firstFollowsSecond: bFollowsA, secondFollowsFirst: aFollowsB}
}

// mutualFollow reports whether a follows b and whether b follows a. Follow
// relationships are cached in memory per unordered handle pair for the
// duration of the event, so the heuristic PairScore and the LLM follow notes
// share one CheckMutualFollow per pair — one GitHub API call per directed
// follow pair per event. The lock is held across the API call so concurrent
// scorers wait for one check instead of racing duplicate calls.
func (m *Matcher) mutualFollow(a, b string) (aFollowsB, bFollowsA bool) {
	first, second := orderedPair(a, b)
	key := first + ":" + second

	m.followMu.Lock()
	defer m.followMu.Unlock()
	if m.follows == nil {
		m.follows = make(map[string]followCheck)
	}
	if cached, ok := m.follows[key]; ok {
		logger("matcher").Debug("follow cache hit", "pair", key)
		return cached.directed(first == a)
	}
	aFollowsB, bFollowsA = m.github.CheckMutualFollow(a, b)
	m.follows[key] = newFollowCheck(first == a, aFollowsB, bFollowsA)
	return aFollowsB, bFollowsA
}

func (m *Matcher) followNote(p1, p2 *Participant) string {
	if !p1.HasGitHub || !p2.HasGitHub {
		return ""
	}
	aFollowsB, bFollowsA := m.mutualFollow(p1.GitHubHandle, p2.GitHubHandle)
	return followSentence(p1.PersonaName, p2.PersonaName, aFollowsB, bFollowsA)
}

// followSentence describes the pair's follow relationship for the LLM.
func followSentence(nameA, nameB string, aFollowsB, bFollowsA bool) string {
	switch {
	case aFollowsB && bFollowsA:
		return fmt.Sprintf("\n\nNote: %s and %s already follow each other on GitHub!", nameA, nameB)
	case aFollowsB:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", nameA, nameB)
	case bFollowsA:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", nameB, nameA)
	}
	return ""
}

// fmtInterests renders Interests as "category: a, b; ...".
func fmtInterests(interests Interests) string {
	var parts []string
	for _, cat := range []struct {
		name  string
		items []string
	}{
		{"languages", interests.Languages},
		{"tools", interests.Tools},
		{"domains", interests.Domains},
	} {
		if len(cat.items) > 0 {
			parts = append(parts, cat.name+": "+strings.Join(cat.items, ", "))
		}
	}
	return strings.Join(parts, "; ")
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// PairScore calculates a numeric compatibility score between two participants.
func (m *Matcher) PairScore(a, b *Participant) int {
	if a.Profile == nil || b.Profile == nil {
		return 0
	}
	return profileScore(a.Profile, b.Profile) + answerScore(a, b) + m.followScore(a, b)
}

// profileScore scores what the two profiles share: +3 per shared language,
// +2 per shared topic, +2 for the same project type, +1 per shared dev
// environment.
func profileScore(aP, bP *GitHubProfile) int {
	return 3*countShared(aP.Languages, bP.Languages) +
		2*countShared(aP.TopTopics, bP.TopTopics) +
		projectTypeScore(aP, bP) +
		countShared(devEnvironments(aP), devEnvironments(bP))
}

// countShared counts the values of other that also appear in set.
func countShared(set, other []string) int {
	shared := make(map[string]bool, len(set))
	for _, v := range set {
		shared[v] = true
	}
	n := 0
	for _, v := range other {
		if shared[v] {
			n++
		}
	}
	return n
}

// projectTypeScore is +2 when both chose the same non-empty project type.
func projectTypeScore(aP, bP *GitHubProfile) int {
	if aP.ExtraAnswers == nil || bP.ExtraAnswers == nil {
		return 0
	}
	if aP.ExtraAnswers.ProjectType != "" && aP.ExtraAnswers.ProjectType == bP.ExtraAnswers.ProjectType {
		return 2
	}
	return 0
}

// devEnvironments lists the profile's dev environments; the Extra Questions
// may not have been asked.
func devEnvironments(p *GitHubProfile) []string {
	if p.ExtraAnswers == nil {
		return nil
	}
	return p.ExtraAnswers.DevEnvironment
}

// answerScore is +1 per interview question the pair answered identically.
func answerScore(a, b *Participant) int {
	bAns := answersOf(b)
	n := 0
	for k, av := range answersOf(a) {
		if bv, ok := bAns[k]; ok && comparableAnswer(k) && av == bv {
			n++
		}
	}
	return n
}

// answersOf returns the Participant's interview answers; a Participant who
// answered none has an empty set.
func answersOf(p *Participant) map[string]string {
	if p.Answers == nil {
		return map[string]string{}
	}
	return p.Answers
}

// followScore scores the GitHub follow relationship: +5 if mutual, +3 if
// one-way.
func (m *Matcher) followScore(a, b *Participant) int {
	if !a.HasGitHub || !b.HasGitHub {
		return 0
	}
	return followPoints(m.mutualFollow(a.GitHubHandle, b.GitHubHandle))
}

// followPoints maps a follow relationship onto its score.
func followPoints(aFollowsB, bFollowsA bool) int {
	if aFollowsB && bFollowsA {
		return 5
	}
	if aFollowsB != bFollowsA {
		return 3
	}
	return 0
}

// Match is a Pair chosen by the Matcher, with its LLM assessment.
type Match struct {
	A, B   *Participant
	Result *matchResult
}

// MatchPool pairs the Participants using the three-phase algorithm from ADR-0001:
//  1. heuristic: each Participant's top-5 candidates by PairScore become candidate Pairs
//  2. LLM: every candidate Pair is assessed (two at a time, cached)
//  3. greedy: Pairs are taken by descending LLM score while both Participants are free
//
// Participants left over are paired by heuristic and assessed. Each Participant
// ends up in at most one Match. When the LLM fails, the heuristic-derived
// fallback assessment is used.
func (m *Matcher) MatchPool(participants []*Participant) []Match {
	if len(participants) < 2 {
		return nil
	}
	candidates := m.candidatePairs(participants)
	m.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidate pairs...", len(candidates)))
	matches := greedyTake(byScoreDesc(m.assessAll(candidates)))
	return append(matches, m.assessAll(m.greedyMatch(unpairedParticipants(participants, matches)))...)
}

// greedyTake pairs the assessed candidates by descending LLM score while
// both Participants are free.
func greedyTake(scored []Match) []Match {
	var matches []Match
	taken := map[string]bool{}
	for _, s := range scored {
		if !taken[s.A.ID] && !taken[s.B.ID] {
			matches = append(matches, s)
			taken[s.A.ID], taken[s.B.ID] = true, true
		}
	}
	return matches
}

// pairedIDs lists the Participants already in one of the Matches.
func pairedIDs(matches []Match) map[string]bool {
	taken := map[string]bool{}
	for _, s := range matches {
		taken[s.A.ID], taken[s.B.ID] = true, true
	}
	return taken
}

// unpairedParticipants lists the Participants no Match took.
func unpairedParticipants(participants []*Participant, matches []Match) []*Participant {
	taken := pairedIDs(matches)
	var leftover []*Participant
	for _, p := range participants {
		if !taken[p.ID] {
			leftover = append(leftover, p)
		}
	}
	return leftover
}

// MatchNewcomer finds a partner for a Participant who just became ready, among
// the other ready or matched Participants. Unmatched Participants come first:
// the best LLM-assessed of the newcomer's top candidates. When everyone is
// matched, the newcomer takes over the Match of the candidate whose assessment
// with the newcomer beats that candidate's current Match score, preferring the
// highest assessment. The returned Match's B may still be matched; storing it
// through the Relationship module breaks that Match. Returns nil when there is
// no suitable partner.
func (m *Matcher) MatchNewcomer(newcomer *Participant, others []*Participant) *Match {
	var unmatched, matched []*Participant
	for _, p := range others {
		switch {
		case p.ID == newcomer.ID:
		case p.MatchedWith == "":
			unmatched = append(unmatched, p)
		default:
			matched = append(matched, p)
		}
	}
	if len(unmatched) > 0 {
		return m.bestFor(newcomer, unmatched, func(Match) bool { return true })
	}
	return m.bestFor(newcomer, matched, func(c Match) bool { return c.Result.Score > c.B.Assessment.Score })
}

// bestFor assesses the newcomer against their top candidates in pool and
// returns the highest-scoring eligible Match, or nil.
func (m *Matcher) bestFor(newcomer *Participant, pool []*Participant, eligible func(Match) bool) *Match {
	var pairs [][2]*Participant
	for _, c := range m.topCandidates(newcomer, pool) {
		pairs = append(pairs, [2]*Participant{newcomer, c})
	}
	scored := byScoreDesc(m.assessAll(pairs))
	for _, c := range scored {
		if eligible(c) {
			return &c
		}
	}
	return nil
}

// byScoreDesc orders Matches by descending LLM score, keeping ties in candidate order.
func byScoreDesc(matches []Match) []Match {
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].Result.Score > matches[j].Result.Score })
	return matches
}

// assessAll assesses the Pairs, two LLM calls at a time, logging each to the activity ticker.
func (m *Matcher) assessAll(pairs [][2]*Participant) []Match {
	out := make([]Match, len(pairs))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 2)
	for i, pair := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			result := m.assessOrDefault(pair[0], pair[1])
			<-sem
			out[i] = Match{A: pair[0], B: pair[1], Result: result}
			m.db.LogActivity(fmt.Sprintf("🤝 %s ↔ %s: %d%%", pair[0].PersonaName, pair[1].PersonaName, result.Score))
		}()
	}
	wg.Wait()
	return out
}

func (m *Matcher) assessOrDefault(a, b *Participant) *matchResult {
	result, err := m.ScorePair(a, b)
	if err != nil {
		logger("matcher").Warn("scoring the pair", "a", a.GitHubHandle, "b", b.GitHubHandle, "err", err)
		return m.heuristicMatchResult(a, b)
	}
	return result
}

// topCandidates returns up to 5 Participants from all with the highest PairScore against p.
func (m *Matcher) topCandidates(p *Participant, all []*Participant) []*Participant {
	type scored struct {
		p     *Participant
		score int
	}
	var candidates []scored
	for _, other := range all {
		if other.ID != p.ID {
			candidates = append(candidates, scored{other, m.PairScore(p, other)})
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })

	var out []*Participant
	for i := 0; i < len(candidates) && i < 5; i++ {
		out = append(out, candidates[i].p)
	}
	return out
}

// candidatePairs collects each ready Participant's top candidates as unique Pairs.
func (m *Matcher) candidatePairs(participants []*Participant) [][2]*Participant {
	var pairs [][2]*Participant
	seen := map[string]bool{}
	for _, p := range participants {
		pairs = append(pairs, m.readyPairs(p, participants, seen)...)
	}
	return pairs
}

// readyPairs collects p's top ready candidates as unique Pairs.
func (m *Matcher) readyPairs(p *Participant, all []*Participant, seen map[string]bool) [][2]*Participant {
	if p.PipelineStep != StepReady {
		return nil
	}
	var pairs [][2]*Participant
	for _, candidate := range m.topCandidates(p, all) {
		if candidate.PipelineStep != StepReady || seen[pairKey(p, candidate)] {
			continue
		}
		seen[pairKey(p, candidate)] = true
		pairs = append(pairs, [2]*Participant{p, candidate})
	}
	return pairs
}

// greedyMatch pairs participants by maximum heuristic PairScore.
func (m *Matcher) greedyMatch(participants []*Participant) [][2]*Participant {
	matched := make([]bool, len(participants))
	var pairs [][2]*Participant
	for i, p := range participants {
		if matched[i] {
			continue
		}
		if j := m.bestPartner(i, participants, matched); j >= 0 {
			pairs = append(pairs, [2]*Participant{p, participants[j]})
			matched[i], matched[j] = true, true
		}
	}
	return pairs
}

// bestPartner finds the unmatched Participant (other than i themselves) with
// the highest heuristic PairScore against i, or -1 when there is none.
func (m *Matcher) bestPartner(i int, participants []*Participant, matched []bool) int {
	bestScore, bestIdx := -1, -1
	for j, other := range participants {
		if i == j || matched[j] {
			continue
		}
		if score := m.PairScore(participants[i], other); score > bestScore {
			bestScore, bestIdx = score, j
		}
	}
	return bestIdx
}

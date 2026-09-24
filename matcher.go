package main

import (
	"encoding/json"
	"fmt"
	"log"
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
// compatibility assessment (prompt, parsing, validation) and the two-level
// cache of assessments from ADR-0002.
type Matcher struct {
	db     *DB
	github GitHubAPI
	llm    LLM

	mu    sync.Mutex
	cache map[string]*matchResult // in-memory level; the llm_cache table is the persistent level
}

// NewMatcher creates a new Matcher instance.
func NewMatcher(db *DB, github GitHubAPI, llm LLM) *Matcher {
	return &Matcher{
		db:     db,
		github: github,
		llm:    llm,
		cache:  make(map[string]*matchResult),
	}
}

// defaultMatchResult returns a default match result when LLM scoring fails.
func defaultMatchResult() *matchResult {
	return &matchResult{
		Score:       42,
		Reason:      "The algorithm has spoken. We cannot explain.",
		RedFlags:    []string{},
		GreenFlags:  []string{"You're both here tonight"},
		Icebreakers: []string{"What brings you to this meetup?", "What are you currently building?", "Best tech talk you've seen recently?"},
	}
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
		return cached, nil
	}
	if cached, ok := m.db.GetLLMCache(key); ok {
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

// ClearCache forgets every cached assessment, in memory and in the database.
func (m *Matcher) ClearCache() {
	m.mu.Lock()
	m.cache = make(map[string]*matchResult)
	m.mu.Unlock()
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

func (m *Matcher) assess(p1, p2 *Participant) (*matchResult, error) {
	system := `You are the matchmaker at a tech meetup blind date event.
Analyze two developers' profiles and produce a fun, humorous compatibility assessment.
Respond with ONLY valid JSON — no markdown:
{"score": <0-100>, "reason": "<one funny sentence max 80 chars>", "red_flags": ["...", "..."], "green_flags": ["...", "..."], "icebreakers": ["<question one can ask the other>", "<question>", "<question>"]}`

	user := "Compare these two developers:\n\n" +
		describeDeveloper(1, p1) + "\n\n" + describeDeveloper(2, p2) + m.followNote(p1, p2)

	response, err := m.llm.Chat(system, user)
	if err != nil {
		return nil, err
	}

	var reply struct {
		Score       *int     `json:"score"`
		Reason      string   `json:"reason"`
		RedFlags    []string `json:"red_flags"`
		GreenFlags  []string `json:"green_flags"`
		Icebreakers []string `json:"icebreakers"`
	}
	if err := json.Unmarshal([]byte(extractJSON(response)), &reply); err != nil {
		return nil, fmt.Errorf("match parse error: %v (raw: %s)", err, response)
	}
	if reply.Score == nil {
		return nil, fmt.Errorf("match reply has no score (raw: %s)", response)
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
	profile := p.Profile
	if profile == nil {
		profile = &GitHubProfile{}
	}
	answers := p.Answers
	if answers == nil {
		answers = map[string]string{}
	}
	s := fmt.Sprintf("DEVELOPER %d (%s):\n%s\nInterview answers: %v", n, p.PersonaName, p.Summary(), answers)
	if interests := fmtInterests(p.Interests); interests != "" {
		s += "\nInterests: " + interests
	}
	return s
}

func (m *Matcher) followNote(p1, p2 *Participant) string {
	if !p1.HasGitHub || !p2.HasGitHub {
		return ""
	}
	aFollowsB, bFollowsA := m.github.CheckMutualFollow(p1.GitHubHandle, p2.GitHubHandle)
	switch {
	case aFollowsB && bFollowsA:
		return fmt.Sprintf("\n\nNote: %s and %s already follow each other on GitHub!", p1.PersonaName, p2.PersonaName)
	case aFollowsB:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", p1.PersonaName, p2.PersonaName)
	case bFollowsA:
		return fmt.Sprintf("\n\nNote: %s already follows %s on GitHub.", p2.PersonaName, p1.PersonaName)
	}
	return ""
}

// fmtInterests renders Interests as "category: a, b; ...". Values may be
// []string (freshly computed) or []any (decoded from the database).
func fmtInterests(interests map[string]interface{}) string {
	categories := make([]string, 0, len(interests))
	for category := range interests {
		categories = append(categories, category)
	}
	sort.Strings(categories)

	var parts []string
	for _, category := range categories {
		var items []string
		switch v := interests[category].(type) {
		case []string:
			items = v
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					items = append(items, s)
				}
			}
		}
		if len(items) > 0 {
			parts = append(parts, category+": "+strings.Join(items, ", "))
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
	score := 0

	aP := a.Profile
	if aP == nil {
		return 0
	}
	bP := b.Profile
	if bP == nil {
		return 0
	}

	// Score shared languages: +3 per match
	aLangs := map[string]bool{}
	for _, l := range aP.Languages {
		aLangs[l] = true
	}
	for _, l := range bP.Languages {
		if aLangs[l] {
			score += 3
		}
	}

	// Score shared topics: +2 per match
	aTopics := map[string]bool{}
	for _, t := range aP.TopTopics {
		aTopics[t] = true
	}
	for _, t := range bP.TopTopics {
		if aTopics[t] {
			score += 2
		}
	}

	// Score shared project types: +2 if same
	if aP.ExtraAnswers != nil && bP.ExtraAnswers != nil {
		if aP.ExtraAnswers.ProjectType != "" && aP.ExtraAnswers.ProjectType == bP.ExtraAnswers.ProjectType {
			score += 2
		}
	}

	// Score shared dev environments: +1 per match
	if aP.ExtraAnswers != nil && bP.ExtraAnswers != nil {
		aDevEnv := map[string]bool{}
		for _, e := range aP.ExtraAnswers.DevEnvironment {
			aDevEnv[e] = true
		}
		for _, e := range bP.ExtraAnswers.DevEnvironment {
			if aDevEnv[e] {
				score++
			}
		}
	}

	// Score matching interview answers: +1 per match
	aAns := a.Answers
	if aAns == nil {
		aAns = map[string]string{}
	}
	bAns := b.Answers
	if bAns == nil {
		bAns = map[string]string{}
	}

	for k, av := range aAns {
		if bv, ok := bAns[k]; ok && av == bv {
			score++
		}
	}

	return score
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
// ends up in at most one Match. When the LLM fails, the default assessment is used.
func (m *Matcher) MatchPool(participants []*Participant) []Match {
	if len(participants) < 2 {
		return nil
	}
	candidates := m.candidatePairs(participants)
	m.db.LogActivity(fmt.Sprintf("🔍 Evaluating %d candidate pairs...", len(candidates)))
	scored := byScoreDesc(m.assessAll(candidates))

	var matches []Match
	taken := map[string]bool{}
	for _, s := range scored {
		if !taken[s.A.ID] && !taken[s.B.ID] {
			matches = append(matches, s)
			taken[s.A.ID], taken[s.B.ID] = true, true
		}
	}

	var leftover []*Participant
	for _, p := range participants {
		if !taken[p.ID] {
			leftover = append(leftover, p)
		}
	}
	return append(matches, m.assessAll(m.greedyMatch(leftover))...)
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
	return m.bestFor(newcomer, matched, func(c Match) bool { return c.Result.Score > c.B.CompatScore })
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
		log.Printf("Match scoring error for %s/%s: %v", a.GitHubHandle, b.GitHubHandle, err)
		return defaultMatchResult()
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
		if p.PipelineStep != StepReady {
			continue
		}
		for _, candidate := range m.topCandidates(p, participants) {
			if candidate.PipelineStep != StepReady || seen[pairKey(p, candidate)] {
				continue
			}
			seen[pairKey(p, candidate)] = true
			pairs = append(pairs, [2]*Participant{p, candidate})
		}
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
		bestScore, bestIdx := -1, -1
		for j, other := range participants {
			if i == j || matched[j] {
				continue
			}
			if score := m.PairScore(p, other); score > bestScore {
				bestScore, bestIdx = score, j
			}
		}
		if bestIdx >= 0 {
			pairs = append(pairs, [2]*Participant{p, participants[bestIdx]})
			matched[i], matched[bestIdx] = true, true
		}
	}
	return pairs
}

package main

import "fmt"

// Activity is the public activity ticker shown on the Big Screen. It is the
// only place ticker text is composed: every event it exposes takes
// persona-safe data (a Persona name, a score, an error) rather than a
// Participant's GitHub handle, real name or internal ID, so no caller can
// put an identity on the ticker even by mistake (#45, #54).
type Activity struct {
	db *DB
}

// NewActivity creates the Activity module.
func NewActivity(db *DB) *Activity {
	return &Activity{db: db}
}

func (a *Activity) FetchingGitHubProfile()   { a.log("🔍 Fetching a GitHub profile...") }
func (a *Activity) ProcessingNonGitHubUser() { a.log("📝 Processing non-GitHub user...") }
func (a *Activity) PreparingQuestions()      { a.log("📝 Preparing interview questions...") }
func (a *Activity) ReadyForInterview()       { a.log("✅ Ready for the interview!") }
func (a *Activity) CraftingPersona()         { a.log("🎭 Crafting a new persona...") }

// ParticipantReady announces a Participant has finished onboarding and joined the Pool.
func (a *Activity) ParticipantReady(persona string) {
	a.log(fmt.Sprintf("✅ %s is ready for matching!", persona))
}

func (a *Activity) MatchmakingStarted() { a.log("🔮 The matchmaker agents are at work...") }
func (a *Activity) RematchComplete()    { a.log("💞 Rematch complete") }

// MatchingAgainstPool announces Continuous Matching has started for a newcomer.
func (a *Activity) MatchingAgainstPool(persona string) {
	a.log(fmt.Sprintf("🔮 Matching %s against existing pool...", persona))
}

// NoMatchYet announces a ready Participant found no available partner this round.
func (a *Activity) NoMatchYet(persona string) {
	a.log(fmt.Sprintf("⏳ %s is ready but no match available yet", persona))
}

// TookOver announces a take-over: newcomer displaced previous from their Match.
func (a *Activity) TookOver(newcomer, previous string) {
	a.log(fmt.Sprintf("🔄 %s took over %s's previous match", newcomer, previous))
}

// Matched announces a stored Match. genuine must be false when the score came
// from the default fallback after a failed LLM assessment, so a guess is
// never announced as a real score (#54).
func (a *Activity) Matched(x, y string, score int, genuine bool) {
	if !genuine {
		a.log(fmt.Sprintf("💘 %s ↔ %s", x, y))
		return
	}
	a.log(fmt.Sprintf("💘 %s ↔ %s (%d%%)", x, y, score))
}

func (a *Activity) EvaluatingCandidates(n int) {
	a.log(fmt.Sprintf("🔍 Evaluating %d candidate pairs...", n))
}

// Assessed announces one candidate Pair's LLM assessment. Callers must only
// call this for a genuine assessment: a failed one falls back to a default
// score that must never be announced as real (#54).
func (a *Activity) Assessed(x, y string, score int) {
	a.log(fmt.Sprintf("🤝 %s ↔ %s: %d%%", x, y, score))
}

func (a *Activity) Revealed()         { a.log("🎉 The matches are revealed!") }
func (a *Activity) RematchTriggered() { a.log("🔄 Admin triggered full rematch") }

func (a *Activity) RematchFailed(err error) {
	a.log(fmt.Sprintf("⚠️ Rematch failed: %s", err))
}

func (a *Activity) log(message string) {
	a.db.LogActivity(message)
}

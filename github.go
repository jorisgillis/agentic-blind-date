package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// GitHubAPI fetches GitHub profile data and follow relationships.
// GitHubClient is the production adapter; tests use an in-memory fake.
type GitHubAPI interface {
	FetchProfile(handle string) (*GitHubProfile, error)
	CheckMutualFollow(handleA, handleB string) (bool, bool)
}

// GitHubClient provides access to the GitHub API for fetching user profiles and repositories.
type GitHubClient struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// githubTimeout bounds every GitHub call. A hung request must not stall
// Matchmaking, which holds its lock for the whole assessment.
const githubTimeout = 10 * time.Second

// defaultGitHubBaseURL is the real GitHub API, used when no override is set.
const defaultGitHubBaseURL = "https://api.github.com"

// GitHubBaseURL reads the GitHub API base URL override through lookup (never
// the process environment directly, so this stays testable). Unset means the
// real API; a trailing slash is trimmed so call sites can join with "/".
func GitHubBaseURL(lookup func(string) string) string {
	if v := strings.TrimRight(strings.TrimSpace(lookup("GITHUB_BASE_URL")), "/"); v != "" {
		return v
	}
	return defaultGitHubBaseURL
}

// NewGitHubClient creates a new GitHubClient with the given API token. An
// empty baseURL is the real GitHub API; otherwise every call goes to baseURL
// (the GITHUB_BASE_URL override), so the E2E journeys can run against a local
// stub.
func NewGitHubClient(token, baseURL string) *GitHubClient {
	if baseURL == "" {
		baseURL = defaultGitHubBaseURL
	}
	return &GitHubClient{token: token, baseURL: baseURL, httpClient: &http.Client{Timeout: githubTimeout}}
}

// GitHubProfile contains data fetched from a user's GitHub account.
type GitHubProfile struct {
	Login            string        `json:"login"`
	Name             string        `json:"name"`
	Bio              string        `json:"bio"`
	Company          string        `json:"company"`
	Location         string        `json:"location"`
	PublicRepos      int           `json:"public_repos"`
	Followers        int           `json:"followers"`
	AccountAgeDays   int           `json:"account_age_days"`
	TotalStars       int           `json:"total_stars"`
	Languages        []string      `json:"languages"`
	TopTopics        []string      `json:"top_topics"`
	HasProfileReadme bool          `json:"has_profile_readme"`
	TopRepos         []RepoInfo    `json:"top_repos"`
	ExtraAnswers     *ExtraAnswers `json:"extra_answers,omitempty"`
}

// ExtraAnswers contains profile data for non-GitHub users who manually enter their information.
type ExtraAnswers struct {
	Languages      []string `json:"languages"`
	ProjectType    string   `json:"project_type"`
	DevEnvironment []string `json:"dev_environment"`
	WeirdestBug    string   `json:"weirdest_bug"`
	Keyboard       string   `json:"keyboard"`
}

// RepoInfo contains information about a GitHub repository.
type RepoInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Stars       int    `json:"stars"`
	Language    string `json:"language"`
}

func (g *GitHubClient) FetchProfile(handle string) (*GitHubProfile, error) {
	user, err := g.fetchUser(handle)
	if err != nil {
		return nil, err
	}
	repos, _ := g.fetchRepos(handle) // best-effort
	profile := profileFromUser(user)
	profile.aggregateRepos(repos, handle)
	return profile, nil
}

// profileFromUser copies the user record into a GitHubProfile, computing the
// account age from the account's creation date.
func profileFromUser(user *ghUser) *GitHubProfile {
	profile := &GitHubProfile{
		Login:       user.Login,
		Name:        user.Name,
		Bio:         user.Bio,
		Company:     strings.TrimPrefix(strings.TrimSpace(user.Company), "@"),
		Location:    user.Location,
		PublicRepos: user.PublicRepos,
		Followers:   user.Followers,
	}
	if t, err := time.Parse(time.RFC3339, user.CreatedAt); err == nil {
		profile.AccountAgeDays = int(time.Since(t).Hours() / 24)
	}
	return profile
}

// aggregateRepos folds the user's own repositories into the profile: the
// total star count, the profile README, the languages, the topics and the
// top repos.
func (p *GitHubProfile) aggregateRepos(repos []ghRepo, handle string) {
	topicCount := map[string]int{}
	langSeen := map[string]bool{}
	for _, r := range repos {
		p.foldRepo(r, handle, topicCount, langSeen)
	}
	p.TopTopics = topTopics(topicCount)
}

// foldRepo folds one repository into the profile: its stars, the profile
// README it may be, its language, its topics and, when notable, its place in
// the top repos.
func (p *GitHubProfile) foldRepo(r ghRepo, handle string, topicCount map[string]int, langSeen map[string]bool) {
	p.TotalStars += r.StargazersCount
	if strings.EqualFold(r.Name, handle) {
		p.HasProfileReadme = true
	}
	p.noteLanguage(r.Language, langSeen)
	for _, t := range r.Topics {
		topicCount[t]++
	}
	p.maybeTopRepo(r)
}

// noteLanguage records the repository's language when it has one the profile
// has not seen yet.
func (p *GitHubProfile) noteLanguage(language string, langSeen map[string]bool) {
	if language != "" && !langSeen[language] {
		p.Languages = append(p.Languages, language)
		langSeen[language] = true
	}
}

// maybeTopRepo adds the repository to the top repos when there is room and it
// is notable: it has stars or a description.
func (p *GitHubProfile) maybeTopRepo(r ghRepo) {
	if len(p.TopRepos) >= 5 || (r.StargazersCount == 0 && r.Description == "") {
		return
	}
	p.TopRepos = append(p.TopRepos, RepoInfo{
		Name:        r.Name,
		Description: r.Description,
		Stars:       r.StargazersCount,
		Language:    r.Language,
	})
}

// topTopics lists the up-to-five most frequent topics, most frequent first.
func topTopics(topicCount map[string]int) []string {
	type kv struct {
		k string
		v int
	}
	var topicList []kv
	for k, v := range topicCount {
		topicList = append(topicList, kv{k, v})
	}
	sort.Slice(topicList, func(i, j int) bool { return topicList[i].v > topicList[j].v })
	var out []string
	for i, item := range topicList {
		if i >= 5 {
			break
		}
		out = append(out, item.k)
	}
	return out
}

// Summary describes a Participant for the LLM: their GitHub data if they have
// a GitHub account, and their ExtraAnswers if they gave any.
func (p *Participant) Summary() string {
	if p.Profile == nil {
		return ""
	}
	var parts []string
	if p.HasGitHub {
		parts = append(parts, p.Profile.githubLines()...)
	}
	parts = append(parts, p.Profile.extraLines()...)
	return strings.Join(parts, "\n")
}

// Summary describes a GitHub user's profile for the LLM: GitHub data and any ExtraAnswers.
func (p *GitHubProfile) Summary() string {
	return strings.Join(append(p.githubLines(), p.extraLines()...), "\n")
}

// appendIf appends line to parts when cond holds.
func appendIf(parts []string, cond bool, line string) []string {
	if !cond {
		return parts
	}
	return append(parts, line)
}

func (p *GitHubProfile) githubLines() []string {
	var parts []string
	parts = appendIf(parts, p.Login != "", fmt.Sprintf("GitHub: @%s", p.Login))
	parts = appendIf(parts, p.Name != "", "Name: "+p.Name)
	parts = appendIf(parts, p.Bio != "", "Bio: "+p.Bio)
	parts = appendIf(parts, p.Company != "", "Company: "+p.Company)
	parts = appendIf(parts, p.Location != "", "Location: "+p.Location)
	parts = appendIf(parts, p.AccountAgeDays > 0, fmt.Sprintf("Account age: %d days (~%d years)", p.AccountAgeDays, p.AccountAgeDays/365))
	parts = append(parts, fmt.Sprintf("Public repos: %d, Followers: %d, Total stars: %d", p.PublicRepos, p.Followers, p.TotalStars))
	parts = appendIf(parts, p.HasProfileReadme, "Has profile README: yes")
	parts = appendIf(parts, len(p.Languages) > 0, "Languages used: "+strings.Join(p.Languages, ", "))
	parts = appendIf(parts, len(p.TopTopics) > 0, "Top topics: "+strings.Join(p.TopTopics, ", "))
	return append(parts, repoLines(p.TopRepos)...)
}

// repoLines describes each repo for the LLM.
func repoLines(repos []RepoInfo) []string {
	var parts []string
	for _, r := range repos {
		parts = append(parts, repoLine(r))
	}
	return parts
}

// repoLine describes one repo: its name, description, language and stars.
func repoLine(r RepoInfo) string {
	line := "Repo: " + r.Name
	if r.Description != "" {
		line += " — " + r.Description
	}
	if r.Language != "" {
		line += " (" + r.Language + ")"
	}
	if r.Stars > 0 {
		line += fmt.Sprintf(" ⭐%d", r.Stars)
	}
	return line
}

func (p *GitHubProfile) extraLines() []string {
	var parts []string
	if p.ExtraAnswers == nil {
		return parts
	}
	ea := p.ExtraAnswers
	parts = appendIf(parts, len(ea.Languages) > 0, "Languages: "+strings.Join(ea.Languages, ", "))
	parts = appendIf(parts, ea.ProjectType != "", "Project type: "+ea.ProjectType)
	parts = appendIf(parts, len(ea.DevEnvironment) > 0, "Dev environment: "+strings.Join(ea.DevEnvironment, ", "))
	parts = appendIf(parts, ea.WeirdestBug != "", "Weirdest bug: "+ea.WeirdestBug)
	parts = appendIf(parts, ea.Keyboard != "", "Keyboard: "+ea.Keyboard)
	return parts
}

type ghUser struct {
	Login       string `json:"login"`
	Name        string `json:"name"`
	Bio         string `json:"bio"`
	Company     string `json:"company"`
	Location    string `json:"location"`
	CreatedAt   string `json:"created_at"`
	PublicRepos int    `json:"public_repos"`
	Followers   int    `json:"followers"`
}

type ghRepo struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	StargazersCount int      `json:"stargazers_count"`
	Language        string   `json:"language"`
	Fork            bool     `json:"fork"`
	Topics          []string `json:"topics"`
}

func (g *GitHubClient) get(url string) ([]byte, error) {
	req, _ := http.NewRequest("GET", url, nil) //nolint:gosec // G704: the URL is the operator-configured base plus a url.PathEscape'd handle, not user input
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.httpClient.Do(req) //nolint:gosec // G704: carries the operator-configured base URL, not user input
	if err != nil {
		return nil, err
	}
	// The body is drained below before the return, so the close cannot matter.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("GitHub user not found: %s", url)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API error %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (g *GitHubClient) fetchUser(handle string) (*ghUser, error) {
	body, err := g.get(g.baseURL + "/users/" + url.PathEscape(handle))
	if err != nil {
		return nil, err
	}
	var u ghUser
	return &u, json.Unmarshal(body, &u)
}

func (g *GitHubClient) fetchRepos(handle string) ([]ghRepo, error) {
	body, err := g.get(g.baseURL + "/users/" + url.PathEscape(handle) + "/repos?sort=stars&per_page=30")
	if err != nil {
		return nil, err
	}
	var repos []ghRepo
	if err := json.Unmarshal(body, &repos); err != nil {
		return nil, err
	}
	var own []ghRepo
	for _, r := range repos {
		if !r.Fork {
			own = append(own, r)
		}
	}
	return own, nil
}

// checkFollows returns true if follower follows followee on GitHub.
// GitHub returns 204 (following) or 404 (not following).
func (g *GitHubClient) checkFollows(follower, followee string) bool {
	endpoint := g.baseURL + "/users/" +
		url.PathEscape(follower) + "/following/" + url.PathEscape(followee)
	req, _ := http.NewRequest("GET", endpoint, nil) //nolint:gosec // G704: the operator-configured base plus url.PathEscape'd handles, not user input
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.httpClient.Do(req) //nolint:gosec // G704: carries the operator-configured base URL, not user input
	if err != nil {
		return false
	}
	_ = resp.Body.Close() // only the status was read; the close cannot matter
	return resp.StatusCode == 204
}

// CheckMutualFollow returns (aFollowsB, bFollowsA).
func (g *GitHubClient) CheckMutualFollow(handleA, handleB string) (bool, bool) {
	return g.checkFollows(handleA, handleB), g.checkFollows(handleB, handleA)
}

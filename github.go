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
	httpClient *http.Client
}

// githubTimeout bounds every GitHub call. A hung request must not stall
// Matchmaking, which holds its lock for the whole assessment.
const githubTimeout = 10 * time.Second

// NewGitHubClient creates a new GitHubClient with the given API token.
func NewGitHubClient(token string) *GitHubClient {
	return &GitHubClient{token: token, httpClient: &http.Client{Timeout: githubTimeout}}
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

	topicCount := map[string]int{}
	langSeen := map[string]bool{}

	for _, r := range repos {
		profile.TotalStars += r.StargazersCount
		if strings.EqualFold(r.Name, handle) {
			profile.HasProfileReadme = true
		}
		if r.Language != "" && !langSeen[r.Language] {
			profile.Languages = append(profile.Languages, r.Language)
			langSeen[r.Language] = true
		}
		for _, t := range r.Topics {
			topicCount[t]++
		}
		if len(profile.TopRepos) < 5 && (r.StargazersCount > 0 || r.Description != "") {
			profile.TopRepos = append(profile.TopRepos, RepoInfo{
				Name:        r.Name,
				Description: r.Description,
				Stars:       r.StargazersCount,
				Language:    r.Language,
			})
		}
	}

	type kv struct {
		k string
		v int
	}
	var topicList []kv
	for k, v := range topicCount {
		topicList = append(topicList, kv{k, v})
	}
	sort.Slice(topicList, func(i, j int) bool { return topicList[i].v > topicList[j].v })
	for i, item := range topicList {
		if i >= 5 {
			break
		}
		profile.TopTopics = append(profile.TopTopics, item.k)
	}

	return profile, nil
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

func (p *GitHubProfile) githubLines() []string {
	var parts []string
	if p.Login != "" {
		parts = append(parts, fmt.Sprintf("GitHub: @%s", p.Login))
	}
	if p.Name != "" {
		parts = append(parts, "Name: "+p.Name)
	}
	if p.Bio != "" {
		parts = append(parts, "Bio: "+p.Bio)
	}
	if p.Company != "" {
		parts = append(parts, "Company: "+p.Company)
	}
	if p.Location != "" {
		parts = append(parts, "Location: "+p.Location)
	}
	if p.AccountAgeDays > 0 {
		parts = append(parts, fmt.Sprintf("Account age: %d days (~%d years)", p.AccountAgeDays, p.AccountAgeDays/365))
	}
	parts = append(parts, fmt.Sprintf("Public repos: %d, Followers: %d, Total stars: %d", p.PublicRepos, p.Followers, p.TotalStars))
	if p.HasProfileReadme {
		parts = append(parts, "Has profile README: yes")
	}
	if len(p.Languages) > 0 {
		parts = append(parts, "Languages used: "+strings.Join(p.Languages, ", "))
	}
	if len(p.TopTopics) > 0 {
		parts = append(parts, "Top topics: "+strings.Join(p.TopTopics, ", "))
	}
	for _, r := range p.TopRepos {
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
		parts = append(parts, line)
	}
	return parts
}

func (p *GitHubProfile) extraLines() []string {
	var parts []string
	if p.ExtraAnswers != nil {
		ea := p.ExtraAnswers
		if len(ea.Languages) > 0 {
			parts = append(parts, "Languages: "+strings.Join(ea.Languages, ", "))
		}
		if ea.ProjectType != "" {
			parts = append(parts, "Project type: "+ea.ProjectType)
		}
		if len(ea.DevEnvironment) > 0 {
			parts = append(parts, "Dev environment: "+strings.Join(ea.DevEnvironment, ", "))
		}
		if ea.WeirdestBug != "" {
			parts = append(parts, "Weirdest bug: "+ea.WeirdestBug)
		}
		if ea.Keyboard != "" {
			parts = append(parts, "Keyboard: "+ea.Keyboard)
		}
	}
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
	req, _ := http.NewRequest("GET", url, nil) // built from a constant base and an escaped handle
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("GitHub user not found: %s", url)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API error %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func (g *GitHubClient) fetchUser(handle string) (*ghUser, error) {
	body, err := g.get("https://api.github.com/users/" + url.PathEscape(handle))
	if err != nil {
		return nil, err
	}
	var u ghUser
	return &u, json.Unmarshal(body, &u)
}

func (g *GitHubClient) fetchRepos(handle string) ([]ghRepo, error) {
	body, err := g.get("https://api.github.com/users/" + url.PathEscape(handle) + "/repos?sort=stars&per_page=30")
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
	endpoint := "https://api.github.com/users/" +
		url.PathEscape(follower) + "/following/" + url.PathEscape(followee)
	req, _ := http.NewRequest("GET", endpoint, nil) // built from a constant base and escaped handles
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 204
}

// CheckMutualFollow returns (aFollowsB, bFollowsA).
func (g *GitHubClient) CheckMutualFollow(handleA, handleB string) (bool, bool) {
	return g.checkFollows(handleA, handleB), g.checkFollows(handleB, handleA)
}

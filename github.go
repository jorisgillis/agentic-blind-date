package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type GitHubClient struct {
	token string
}

type GitHubProfile struct {
	Login            string     `json:"login"`
	Name             string     `json:"name"`
	Bio              string     `json:"bio"`
	Company          string     `json:"company"`
	Location         string     `json:"location"`
	PublicRepos      int        `json:"public_repos"`
	Followers        int        `json:"followers"`
	AccountAgeDays   int        `json:"account_age_days"`
	TotalStars       int        `json:"total_stars"`
	Languages        []string   `json:"languages"`
	TopTopics        []string   `json:"top_topics"`
	HasProfileReadme bool       `json:"has_profile_readme"`
	TopRepos         []RepoInfo `json:"top_repos"`
}

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

func (p *GitHubProfile) Summary() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("GitHub: @%s", p.Login))
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
	return strings.Join(parts, "\n")
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
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if g.token != "" {
		req.Header.Set("Authorization", "Bearer "+g.token)
	}

	resp, err := http.DefaultClient.Do(req)
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
	body, err := g.get("https://api.github.com/users/" + handle)
	if err != nil {
		return nil, err
	}
	var u ghUser
	return &u, json.Unmarshal(body, &u)
}

func (g *GitHubClient) fetchRepos(handle string) ([]ghRepo, error) {
	body, err := g.get("https://api.github.com/users/" + handle + "/repos?sort=stars&per_page=30")
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

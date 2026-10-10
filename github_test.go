package main

import (
	"strings"
	"testing"
)

func TestNewGitHubClient(t *testing.T) {
	// Test with empty token
	client := NewGitHubClient("")
	if client.token != "" {
		t.Errorf("expected empty token, got %s", client.token)
	}

	// Test with non-empty token
	client = NewGitHubClient("test-token")
	if client.token != "test-token" {
		t.Errorf("expected token 'test-token', got %s", client.token)
	}
}

func TestGitHubProfileSummary(t *testing.T) {
	// Test with minimal profile
	profile := &GitHubProfile{Login: "testuser"}
	summary := profile.Summary()
	if summary == "" {
		t.Error("expected non-empty summary for profile with login")
	}

	// Test with full profile
	profile = &GitHubProfile{
		Login:          "testuser",
		Name:           "Test User",
		Bio:            "A test user",
		Company:        "Test Corp",
		Location:       "Test City",
		PublicRepos:    10,
		Followers:      100,
		TotalStars:     500,
		Languages:      []string{"Go", "Python"},
		TopTopics:      []string{"web", "api"},
		TopRepos:       []RepoInfo{{Name: "repo1", Stars: 100}, {Name: "repo2", Stars: 50}},
		HasProfileReadme: true,
		AccountAgeDays: 365,
	}
	summary = profile.Summary()
	if summary == "" {
		t.Error("expected non-empty summary for full profile")
	}
}

func TestParticipantSummary_AsksWhetherTheParticipantHasGitHub(t *testing.T) {
	extra := &ExtraAnswers{Languages: []string{"Python"}, ProjectType: "Data Science/Engineering"}
	ada := &Participant{HasGitHub: false, Profile: &GitHubProfile{Login: "stale-login", ExtraAnswers: extra}}
	octo := &Participant{HasGitHub: true, Profile: &GitHubProfile{Login: "", Languages: []string{"Go"}, ExtraAnswers: extra}}

	if s := ada.Summary(); strings.Contains(s, "GitHub") || !strings.Contains(s, "Languages: Python") {
		t.Errorf("Non-GitHub User: no GitHub section, just their answers:\n%s", s)
	}
	if s := octo.Summary(); !strings.Contains(s, "Languages used: Go") || !strings.Contains(s, "Project type: Data Science/Engineering") {
		t.Errorf("GitHub user: GitHub data (even without a login) plus answers:\n%s", s)
	}
}

func TestSummary_DescribesReposAndHandlesAMissingProfile(t *testing.T) {
	p := &GitHubProfile{Login: "octo", TopRepos: []RepoInfo{{Name: "gopher", Description: "Go things", Language: "Go", Stars: 3}}}
	if s := p.Summary(); !strings.Contains(s, "Repo: gopher — Go things (Go) ⭐3") {
		t.Errorf("repo line: got\n%s", s)
	}
	if s := (&Participant{}).Summary(); s != "" {
		t.Errorf("no profile, no summary: got %q", s)
	}
}

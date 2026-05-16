package main

import (
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

func TestCheckMutualFollow(t *testing.T) {
	// Create a client with empty token (will fail on actual HTTP calls)
	client := NewGitHubClient("")

	// Test with empty handles - returns (false, false)
	aFollowsB, bFollowsA := client.CheckMutualFollow("", "")
	if aFollowsB || bFollowsA {
		t.Error("expected both to be false for empty handles")
	}
}

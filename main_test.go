package main

import (
	"os"
	"strings"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	// Create a temporary .env file
	envContent := `TEST_KEY=test_value
TEST_KEY2="quoted value"
# This is a comment
EMPTY_LINE=
INVALID_LINE
ANOTHER_KEY=another_value
`
	if err := os.WriteFile(".env.test", []byte(envContent), 0644); err != nil {
		t.Fatalf("failed to create .env.test: %v", err)
	}
	defer os.Remove(".env.test")

	// Save original env
	originalEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, e := range originalEnv {
			pair := strings.SplitN(e, "=", 2)
			if len(pair) == 2 {
				os.Setenv(pair[0], pair[1])
			}
		}
	}()

	// Temporarily rename .env to .env.test
	if err := os.Rename(".env", ".env.bak"); err == nil {
		defer os.Rename(".env.bak", ".env")
	}
	if err := os.Rename(".env.test", ".env"); err != nil {
		t.Fatalf("failed to rename .env.test: %v", err)
	}
	defer os.Rename(".env", ".env.test")

	// Clear env and call loadDotEnv
	os.Clearenv()
	loadDotEnv()

	// Check that values were loaded
	if os.Getenv("TEST_KEY") != "test_value" {
		t.Errorf("expected TEST_KEY=test_value, got %s", os.Getenv("TEST_KEY"))
	}
	if os.Getenv("TEST_KEY2") != "quoted value" {
		t.Errorf("expected TEST_KEY2=quoted value, got %s", os.Getenv("TEST_KEY2"))
	}
	if os.Getenv("ANOTHER_KEY") != "another_value" {
		t.Errorf("expected ANOTHER_KEY=another_value, got %s", os.Getenv("ANOTHER_KEY"))
	}
	// EMPTY_LINE should not be set (empty value)
	if os.Getenv("EMPTY_LINE") != "" {
		t.Errorf("expected EMPTY_LINE to be empty, got %s", os.Getenv("EMPTY_LINE"))
	}
	// INVALID_LINE should not be set (no =)
	if os.Getenv("INVALID_LINE") != "" {
		t.Errorf("expected INVALID_LINE to be empty, got %s", os.Getenv("INVALID_LINE"))
	}
}

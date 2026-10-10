package main

import (
	"os"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := t.TempDir() + "/dotenv"
	os.WriteFile(path, []byte(`TEST_KEY=test_value
TEST_KEY2="quoted value"
# This is a comment
EMPTY_LINE=
INVALID_LINE
=no_key
ALREADY_SET=from_file

ANOTHER_KEY=another_value
`), 0644)
	for _, key := range []string{"TEST_KEY", "TEST_KEY2", "EMPTY_LINE", "INVALID_LINE", "ANOTHER_KEY"} {
		t.Setenv(key, "") // restored after the test
		os.Unsetenv(key)
	}
	t.Setenv("ALREADY_SET", "from_environment")

	loadDotEnv(path)

	for key, want := range map[string]string{
		"TEST_KEY":     "test_value",
		"TEST_KEY2":    "quoted value", // quotes stripped
		"ANOTHER_KEY":  "another_value",
		"EMPTY_LINE":   "",
		"INVALID_LINE": "",
		"ALREADY_SET":  "from_environment", // the environment wins
	} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s: want %q, got %q", key, want, got)
		}
	}
}

func TestLoadDotEnv_AMissingFileIsFine(t *testing.T) {
	loadDotEnv(t.TempDir() + "/does-not-exist") // must not panic
}

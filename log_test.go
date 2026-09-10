package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONLLoggerWritesAndRotatesCompleteEvents(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "probe.jsonl")
	logger, err := openJSONLLogger(path, 180, 2)
	if err != nil {
		t.Fatalf("openJSONLLogger() error: %v", err)
	}

	event := map[string]string{"event": "probe_cycle", "payload": strings.Repeat("x", 100)}
	for index := 0; index < 4; index++ {
		if err := logger.Append(event); err != nil {
			t.Fatalf("Append() error: %v", err)
		}
	}
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}

	for _, candidate := range []string{path, path + ".1", path + ".2"} {
		contents, err := os.ReadFile(candidate)
		if err != nil {
			t.Fatalf("ReadFile(%q) error: %v", candidate, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			var decoded map[string]string
			if err := json.Unmarshal([]byte(line), &decoded); err != nil {
				t.Fatalf("invalid JSONL in %q: %v", candidate, err)
			}
		}
	}
}

func TestRedactTargetURLCredentialsAndQuery(t *testing.T) {
	got := redactTargetURL("https://user:secret@example.com/path?token=secret#fragment")
	if got != "https://example.com/path" {
		t.Fatalf("redactTargetURL() = %q", got)
	}
}

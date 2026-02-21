package main

import (
	"strings"
	"testing"
)

func TestParseGitURI(t *testing.T) {
	tests := []struct {
		uri  string
		user string
		repo string
		ok   bool
	}{
		{"https://github.com/tobi/try.git", "tobi", "try", true},
		{"git@github.com:tobi/try.git", "tobi", "try", true},
		{"https://gitlab.com/foo/bar", "foo", "bar", true},
		{"not-a-uri", "", "", false},
	}

	for _, tt := range tests {
		got, ok := parseGitURI(tt.uri)
		if ok != tt.ok {
			t.Fatalf("parseGitURI(%q) ok=%v want %v", tt.uri, ok, tt.ok)
		}
		if !tt.ok {
			continue
		}
		if got.User != tt.user || got.Repo != tt.repo {
			t.Fatalf("parseGitURI(%q)=%+v want user=%s repo=%s", tt.uri, got, tt.user, tt.repo)
		}
	}
}

func TestGenerateCloneDirectoryNameCustomName(t *testing.T) {
	got, err := generateCloneDirectoryName("https://github.com/tobi/try.git", "my custom")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != "my-custom" {
		t.Fatalf("got %q want %q", got, "my-custom")
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("a'b")
	if !strings.HasPrefix(got, "'a") || !strings.HasSuffix(got, "b'") || !strings.Contains(got, "\"'\"'\"") {
		t.Fatalf("got %q", got)
	}
}

func TestInitScriptUsesExecMode(t *testing.T) {
	script := initScript("/tmp/try", "/tmp/tries")
	if !strings.Contains(script, "exec --path '/tmp/tries'") {
		t.Fatalf("init script missing exec/path wiring: %s", script)
	}
}

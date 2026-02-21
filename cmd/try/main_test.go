package main

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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

func TestScriptDeleteUsesGuardedCommands(t *testing.T) {
	cmds := scriptDelete("/tmp/tries/alpha", "/tmp/tries")
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "rm -rf 'alpha'") {
		t.Fatalf("delete script missing target: %s", joined)
	}
	if !strings.Contains(joined, "cd '/tmp/tries'") {
		t.Fatalf("delete script missing base cd: %s", joined)
	}
}

func TestSelectorCtrlDRequiresYES(t *testing.T) {
	target := filepath.Join("/tmp/tries", "alpha")
	m := selectorModel{
		basePath: "/tmp/tries",
		filtered: []scoredEntry{{entry: entry{Name: "alpha", Path: target}}},
	}

	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	m1 := model.(selectorModel)
	if !m1.deleteMode {
		t.Fatalf("expected delete mode after Ctrl+D")
	}
	if m1.deleteTarget != target {
		t.Fatalf("unexpected delete target: %s", m1.deleteTarget)
	}

	model, _ = m1.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("YES")})
	model, _ = model.(selectorModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m2 := model.(selectorModel)
	if m2.deleted != target {
		t.Fatalf("expected deleted target %s, got %s", target, m2.deleted)
	}
}

func TestSelectorCtrlDRejectsNonYES(t *testing.T) {
	target := filepath.Join("/tmp/tries", "alpha")
	m := selectorModel{
		basePath: "/tmp/tries",
		filtered: []scoredEntry{{entry: entry{Name: "alpha", Path: target}}},
	}
	model, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlD})
	model, _ = model.(selectorModel).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("no")})
	model, _ = model.(selectorModel).Update(tea.KeyMsg{Type: tea.KeyEnter})
	m1 := model.(selectorModel)
	if m1.deleted != "" {
		t.Fatalf("expected no deletion when confirmation is not YES")
	}
	if !m1.deleteMode {
		t.Fatalf("expected to remain in delete mode on invalid confirmation")
	}
}

package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	version       = "1.8.2"
	scriptWarning = "# if you can read this, you didn't launch try from an alias. run try --help."
)

var (
	httpsGitURIRe = regexp.MustCompile(`^https?://([^/]+)/([^/]+)/([^/]+)$`)
	sshGitURIRe   = regexp.MustCompile(`^git@([^:]+):([^/]+)/([^/]+)$`)

	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	subtleStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selectStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	createStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	dangerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
	confirmStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("213")).Bold(true)
)

type entry struct {
	Name    string
	Path    string
	Created time.Time
	Touched time.Time
}

type scoredEntry struct {
	entry
	Score      float64
	Highlights []int
}

type gitURI struct {
	Host string
	User string
	Repo string
}

type selectorModel struct {
	basePath      string
	query         string
	entries       []entry
	filtered      []scoredEntry
	cursor        int
	selected      string
	deleted       string
	cancelled     bool
	deleteMode    bool
	deleteConfirm string
	deleteTarget  string
	keys          selectorKeyMap
	help          help.Model
	width         int
	height        int
}

type selectorKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Delete  key.Binding
	Back    key.Binding
	Confirm key.Binding
	Cancel  key.Binding
}

func (k selectorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Enter, k.Delete, k.Cancel}
}

func (k selectorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter},
		{k.Delete, k.Back, k.Confirm, k.Cancel},
	}
}

func newSelectorKeyMap() selectorKeyMap {
	return selectorKeyMap{
		Up:      key.NewBinding(key.WithKeys("up", "ctrl+p"), key.WithHelp("↑/ctrl+p", "up")),
		Down:    key.NewBinding(key.WithKeys("down", "ctrl+n"), key.WithHelp("↓/ctrl+n", "down")),
		Enter:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "select")),
		Delete:  key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "delete")),
		Back:    key.NewBinding(key.WithKeys("backspace"), key.WithHelp("backspace", "erase")),
		Confirm: key.NewBinding(key.WithKeys("YES"), key.WithHelp("YES", "confirm delete")),
		Cancel:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

func defaultTryPath() string {
	if v := strings.TrimSpace(os.Getenv("TRY_PATH")); v != "" {
		return mustExpand(v)
	}
	return mustExpand("~/src/tries")
}

func mustExpand(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `"'"'"`) + "'"
}

func emitScript(w io.Writer, cmds []string) {
	fmt.Fprintln(w, scriptWarning)
	for i, cmd := range cmds {
		if i == 0 {
			fmt.Fprint(w, cmd)
		} else {
			fmt.Fprint(w, "  "+cmd)
		}
		if i < len(cmds)-1 {
			fmt.Fprintln(w, " && \\")
		} else {
			fmt.Fprintln(w)
		}
	}
}

func scriptCD(path string) []string {
	q := shellQuote(path)
	return []string{"touch " + q, "echo " + q, "cd " + q}
}

func scriptMkdirCD(path string) []string {
	return append([]string{"mkdir -p " + shellQuote(path)}, scriptCD(path)...)
}

func scriptClone(path, uri string) []string {
	msg := fmt.Sprintf("Using git clone to create this trial from %s.", uri)
	cmds := []string{
		"mkdir -p " + shellQuote(path),
		"echo " + shellQuote(msg),
		"git clone " + shellQuote(uri) + " " + shellQuote(path),
	}
	return append(cmds, scriptCD(path)...)
}

func scriptDelete(path, basePath string) []string {
	base := filepath.Base(path)
	qBasePath := shellQuote(basePath)
	return []string{
		"old_pwd=$PWD",
		"cd " + qBasePath,
		"test -d " + shellQuote(base) + " && rm -rf " + shellQuote(base),
		"cd \"$old_pwd\" 2>/dev/null || cd " + qBasePath,
	}
}

func parseGitURI(uri string) (*gitURI, bool) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(uri), ".git")
	if trimmed == "" {
		return nil, false
	}
	if m := httpsGitURIRe.FindStringSubmatch(trimmed); len(m) == 4 {
		return &gitURI{Host: m[1], User: m[2], Repo: m[3]}, true
	}
	if m := sshGitURIRe.FindStringSubmatch(trimmed); len(m) == 4 {
		return &gitURI{Host: m[1], User: m[2], Repo: m[3]}, true
	}
	return nil, false
}

func isGitURI(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return false
	}
	if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") || strings.HasPrefix(arg, "git@") {
		return true
	}
	return strings.Contains(arg, "github.com") || strings.Contains(arg, "gitlab.com") || strings.HasSuffix(arg, ".git")
}

func generateCloneDirectoryName(uri, customName string) (string, error) {
	if strings.TrimSpace(customName) != "" {
		return sanitizeName(customName), nil
	}
	parsed, ok := parseGitURI(uri)
	if !ok {
		return "", fmt.Errorf("unable to parse git URI: %s", uri)
	}
	return fmt.Sprintf("%s-%s-%s", time.Now().Format("2006-01-02"), parsed.User, parsed.Repo), nil
}

func sanitizeName(name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(name)), "-")
}

func printHelp(w io.Writer) {
	fmt.Fprintf(w, `try v%s - ephemeral workspace manager

Usage:
  try [query]           Interactive directory selector
  try clone <url>       Clone repo into dated directory
  try init [path]       Output shell function definition
  try --help            Show this help

Environment:
  TRY_PATH          Tries directory (default: ~/src/tries)

Keyboard:
  ↑/↓, Ctrl-P/N     Navigate
  Enter              Select / Create new
  Ctrl-D             Delete selected try (confirm with YES)
  Backspace          Delete character
  Esc                Cancel
`, version)
}

func fishShell() bool {
	shell := os.Getenv("SHELL")
	return strings.Contains(shell, "fish")
}

func initScript(exePath, triesPath string) string {
	pathArg := ""
	if triesPath != "" {
		pathArg = " --path " + shellQuote(triesPath)
	}
	if fishShell() {
		return fmt.Sprintf(`function try
  set -l out (%s exec%s $argv 2>/dev/tty | string collect)
  if test $pipestatus[1] -eq 0
    eval $out
  else
    echo $out
  end
end
`, shellQuote(exePath), pathArg)
	}
	return fmt.Sprintf(`try() {
  local out
  out=$(%s exec%s "$@" 2>/dev/tty)
  if [ $? -eq 0 ]; then
    eval "$out"
  else
    echo "$out"
  fi
}
`, shellQuote(exePath), pathArg)
}

func extractOption(args []string, opt string) ([]string, string) {
	idx := -1
	for i, arg := range args {
		if arg == opt || strings.HasPrefix(arg, opt+"=") {
			idx = i
		}
	}
	if idx == -1 {
		return args, ""
	}
	arg := args[idx]
	args = slices.Delete(args, idx, idx+1)
	if strings.Contains(arg, "=") {
		return args, strings.SplitN(arg, "=", 2)[1]
	}
	if idx >= len(args) {
		return args, ""
	}
	value := args[idx]
	args = slices.Delete(args, idx, idx+1)
	return args, value
}

func cmdClone(args []string, triesPath string) ([]string, error) {
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("git URI required for clone command")
	}
	uri := args[0]
	customName := ""
	if len(args) > 1 {
		customName = strings.Join(args[1:], " ")
	}
	dirName, err := generateCloneDirectoryName(uri, customName)
	if err != nil {
		return nil, err
	}
	return scriptClone(filepath.Join(triesPath, dirName), uri), nil
}

func listEntries(basePath string) ([]entry, error) {
	if err := os.MkdirAll(basePath, 0o755); err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}
	items := make([]entry, 0, len(dirs))
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		full := filepath.Join(basePath, name)
		st, err := os.Stat(full)
		if err != nil {
			continue
		}
		created := st.ModTime()
		if runtime.GOOS != "windows" {
			created = fileCTime(st)
		}
		items = append(items, entry{Name: name, Path: full, Touched: st.ModTime(), Created: created})
	}
	return items, nil
}

func fileCTime(fi os.FileInfo) time.Time {
	return fi.ModTime()
}

func baseScore(e entry) float64 {
	now := time.Now()
	score := 0.0
	if len(e.Name) >= len("2006-01-02-") {
		if _, err := time.Parse("2006-01-02", e.Name[:10]); err == nil && e.Name[10] == '-' {
			score += 2.0
		}
	}
	days := now.Sub(e.Created).Hours() / 24
	if days < 0 {
		days = 0
	}
	score += 2 / sqrt(days+1)
	hours := now.Sub(e.Touched).Hours()
	if hours < 0 {
		hours = 0
	}
	score += 3 / sqrt(hours+1)
	return score
}

func sqrt(v float64) float64 {
	return math.Sqrt(v)
}

func fuzzyScore(text, query string, initial float64) (float64, []int, bool) {
	if query == "" {
		return initial, nil, true
	}
	textLower := strings.ToLower(text)
	queryLower := strings.ToLower(query)
	pos := 0
	last := -1
	score := initial
	highlights := make([]int, 0, len(queryLower))
	for _, qc := range queryLower {
		idx := strings.IndexRune(textLower[pos:], qc)
		if idx < 0 {
			return 0, nil, false
		}
		found := pos + idx
		highlights = append(highlights, found)
		score += 1
		if found == 0 || !isWordChar(textLower[found-1]) {
			score += 1
		}
		if last >= 0 {
			gap := found - last - 1
			score += 1 / mathSqrt(float64(gap+1))
		}
		last = found
		pos = found + 1
	}
	score *= float64(len(queryLower)) / float64(last+1)
	score *= 10.0 / (float64(len(text)) + 10.0)
	return score, highlights, true
}

func isWordChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
}

func mathSqrt(x float64) float64 {
	z := x
	if z == 0 {
		return 0
	}
	for i := 0; i < 10; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

func (m selectorModel) Init() tea.Cmd { return nil }

func (m *selectorModel) refresh() {
	filtered := make([]scoredEntry, 0, len(m.entries))
	for _, e := range m.entries {
		score, highlights, ok := fuzzyScore(e.Name, m.query, baseScore(e))
		if !ok {
			continue
		}
		filtered = append(filtered, scoredEntry{entry: e, Score: score, Highlights: highlights})
	}
	slices.SortFunc(filtered, func(a, b scoredEntry) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	m.filtered = filtered
	maxCursor := len(m.filtered)
	if m.cursor > maxCursor {
		m.cursor = maxCursor
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		if m.deleteMode {
			switch msg.Type {
			case tea.KeyEsc:
				m.deleteMode = false
				m.deleteConfirm = ""
				m.deleteTarget = ""
			case tea.KeyBackspace:
				if m.deleteConfirm != "" {
					m.deleteConfirm = m.deleteConfirm[:len(m.deleteConfirm)-1]
				}
			case tea.KeyRunes:
				var b strings.Builder
				b.Grow(len(m.deleteConfirm) + len(msg.Runes))
				b.WriteString(m.deleteConfirm)
				for _, r := range msg.Runes {
					if r == '\n' || r == '\r' {
						continue
					}
					b.WriteRune(r)
				}
				m.deleteConfirm = b.String()
			case tea.KeyEnter:
				if m.deleteConfirm == "YES" && m.deleteTarget != "" {
					m.deleted = m.deleteTarget
					return m, tea.Quit
				}
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyCtrlD:
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				m.deleteMode = true
				m.deleteConfirm = ""
				m.deleteTarget = m.filtered[m.cursor].Path
			}
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.filtered) {
				m.cursor++
			}
		case tea.KeyBackspace:
			if m.query != "" {
				m.query = m.query[:len(m.query)-1]
				m.refresh()
			}
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				if r == '\n' || r == '\r' {
					continue
				}
				m.query += string(r)
			}
			m.refresh()
		case tea.KeyEnter:
			if m.cursor == len(m.filtered) {
				name := sanitizeName(m.query)
				if name == "" {
					name = "new-try"
				}
				target := filepath.Join(m.basePath, time.Now().Format("2006-01-02")+"-"+name)
				target = uniquePath(target)
				_ = os.MkdirAll(target, 0o755)
				m.selected = target
				return m, tea.Quit
			}
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor].Path
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	var b strings.Builder
	if m.deleteMode {
		target := filepath.Base(m.deleteTarget)
		b.WriteString(dangerStyle.Render("Delete try: " + target))
		b.WriteString("\n")
		b.WriteString(promptStyle.Render("Type YES to confirm: "))
		b.WriteString(confirmStyle.Render(m.deleteConfirm))
		b.WriteString("\n")
		b.WriteString(subtleStyle.Render(m.help.View(m.keys)))
		return b.String()
	}

	b.WriteString(titleStyle.Render("try » "))
	if m.query == "" {
		b.WriteString("\n")
	} else {
		b.WriteString(confirmStyle.Render(m.query))
		b.WriteString("\n")
	}
	maxRows := len(m.filtered)
	if m.height > 4 && maxRows > m.height-4 {
		maxRows = m.height - 4
	}
	for i := 0; i < maxRows; i++ {
		prefix := "  "
		if i == m.cursor {
			prefix = selectStyle.Render("→ ")
		}
		b.WriteString(prefix)
		b.WriteString(m.filtered[i].Name)
		b.WriteString("\n")
	}
	createPrefix := "  "
	if m.cursor == len(m.filtered) {
		createPrefix = selectStyle.Render("→ ")
	}
	label := "+ Create new"
	if m.query != "" {
		label += ": " + m.query
	}
	b.WriteString(createPrefix + createStyle.Render(label) + "\n")
	b.WriteString(subtleStyle.Render(m.help.View(m.keys)))
	return b.String()
}

func uniquePath(path string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", path, i)
		if _, err := os.Stat(candidate); errors.Is(err, os.ErrNotExist) {
			return candidate
		}
	}
}

type selectorResult struct {
	selected  string
	deleted   string
	cancelled bool
}

func runSelector(basePath, initialQuery string) (selectorResult, error) {
	entries, err := listEntries(basePath)
	if err != nil {
		return selectorResult{}, err
	}
	helpModel := help.New()
	helpModel.ShowAll = false
	m := selectorModel{
		basePath: basePath,
		query:    initialQuery,
		entries:  entries,
		width:    80,
		height:   24,
		keys:     newSelectorKeyMap(),
		help:     helpModel,
	}
	m.refresh()
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr), tea.WithInput(os.Stdin))
	finalModel, err := p.Run()
	if err != nil {
		return selectorResult{}, err
	}
	fin := finalModel.(selectorModel)
	return selectorResult{selected: fin.selected, deleted: fin.deleted, cancelled: fin.cancelled}, nil
}

func cmdCD(args []string, triesPath string) ([]string, bool, error) {
	searchTerm := strings.Join(args, " ")
	parts := strings.Fields(searchTerm)
	if len(parts) > 0 && isGitURI(parts[0]) {
		uri := parts[0]
		custom := ""
		if len(parts) > 1 {
			custom = strings.Join(parts[1:], " ")
		}
		dirName, err := generateCloneDirectoryName(uri, custom)
		if err != nil {
			return nil, false, err
		}
		return scriptClone(filepath.Join(triesPath, dirName), uri), false, nil
	}
	result, err := runSelector(triesPath, searchTerm)
	if err != nil {
		return nil, false, err
	}
	if result.cancelled || (result.selected == "" && result.deleted == "") {
		return nil, true, nil
	}
	if result.deleted != "" {
		return scriptDelete(result.deleted, triesPath), false, nil
	}
	return scriptCD(result.selected), false, nil
}

func run(argv []string, stdout, stderr io.Writer) int {
	args := append([]string(nil), argv...)
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			printHelp(stderr)
			return 0
		}
		if arg == "--version" || arg == "-v" {
			fmt.Fprintln(stderr, "try "+version)
			return 0
		}
	}
	var pathOpt string
	args, pathOpt = extractOption(args, "--path")
	triesPath := defaultTryPath()
	if pathOpt != "" {
		triesPath = mustExpand(pathOpt)
	}
	if len(args) == 0 {
		printHelp(stderr)
		return 2
	}

	command := args[0]
	args = args[1:]

	emit := func(cmds []string) {
		emitScript(stdout, cmds)
	}

	switch command {
	case "init":
		exe, _ := os.Executable()
		if len(args) > 0 && strings.HasPrefix(args[0], "/") {
			triesPath = mustExpand(args[0])
		}
		fmt.Fprint(stdout, initScript(exe, triesPath))
		return 0
	case "clone":
		cmds, err := cmdClone(args, triesPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		emit(cmds)
		return 0
	case "exec":
		targetCommand := "cd"
		if len(args) > 0 {
			targetCommand = args[0]
			args = args[1:]
		}
		switch targetCommand {
		case "clone":
			cmds, err := cmdClone(args, triesPath)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			emit(cmds)
			return 0
		case "cd":
			cmds, cancelled, err := cmdCD(args, triesPath)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			if cancelled {
				fmt.Fprintln(stdout, "Cancelled.")
				return 1
			}
			emit(cmds)
			return 0
		default:
			args = append([]string{targetCommand}, args...)
			cmds, cancelled, err := cmdCD(args, triesPath)
			if err != nil {
				fmt.Fprintf(stderr, "Error: %v\n", err)
				return 1
			}
			if cancelled {
				fmt.Fprintln(stdout, "Cancelled.")
				return 1
			}
			emit(cmds)
			return 0
		}
	default:
		cmds, cancelled, err := cmdCD(append([]string{command}, args...), triesPath)
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return 1
		}
		if cancelled {
			fmt.Fprintln(stdout, "Cancelled.")
			return 1
		}
		emit(cmds)
		return 0
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

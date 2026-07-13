package execpolicy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"codeworld/internal/permissions"
)

type Rule struct {
	Pattern  []string
	Decision permissions.DecisionKind
	Source   string
}

type Set struct {
	Rules           []Rule
	HostExecutables map[string][]string
}

type Evaluation struct {
	MatchedRules []MatchedRule `json:"matchedRules"`
	Decision     string        `json:"decision,omitempty"`
}

type MatchedRule struct {
	PrefixRuleMatch PrefixRuleMatch `json:"prefixRuleMatch"`
}

type PrefixRuleMatch struct {
	MatchedPrefix   []string `json:"matchedPrefix"`
	Decision        string   `json:"decision"`
	ResolvedProgram string   `json:"resolvedProgram,omitempty"`
}

type MatchOptions struct {
	ResolveHostExecutables bool
}

type ruleMatch struct {
	rule            Rule
	resolvedProgram string
}

// Load 依次加载用户和项目 rules；项目规则追加在用户规则之后。
func Load(root, home string) (Set, error) {
	var set Set
	for _, dir := range []string{filepath.Join(home, "rules"), filepath.Join(root, ".codeworld", "rules")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return Set{}, err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".rules" {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			loaded, err := loadFile(path)
			if err != nil {
				return Set{}, err
			}
			set.merge(loaded)
		}
	}
	return set, nil
}

// LoadFiles loads explicit rule files in argument order for execpolicy tooling.
func LoadFiles(paths []string) (Set, error) {
	var set Set
	for _, path := range paths {
		loaded, err := loadFile(path)
		if err != nil {
			return Set{}, err
		}
		set.merge(loaded)
	}
	return set, nil
}

// Evaluate reports every matching prefix rule and the strongest decision.
func (s Set) Evaluate(command []string) Evaluation {
	return s.EvaluateWithOptions(command, MatchOptions{})
}

// EvaluateWithOptions evaluates a command with optional host executable resolution.
func (s Set) EvaluateWithOptions(command []string, options MatchOptions) Evaluation {
	result := Evaluation{MatchedRules: []MatchedRule{}}
	priority := 0
	for _, matched := range s.matches(command, options.ResolveHostExecutables) {
		decision, strength := evaluationDecision(matched.rule.Decision)
		result.MatchedRules = append(result.MatchedRules, MatchedRule{PrefixRuleMatch: PrefixRuleMatch{
			MatchedPrefix: append([]string(nil), matched.rule.Pattern...), Decision: decision,
			ResolvedProgram: matched.resolvedProgram,
		}})
		if strength > priority {
			priority, result.Decision = strength, decision
		}
	}
	return result
}

func (s *Set) merge(loaded Set) {
	s.Rules = append(s.Rules, loaded.Rules...)
	if len(loaded.HostExecutables) == 0 {
		return
	}
	if s.HostExecutables == nil {
		s.HostExecutables = map[string][]string{}
	}
	for name, paths := range loaded.HostExecutables {
		s.HostExecutables[name] = append([]string(nil), paths...)
	}
}

func evaluationDecision(decision permissions.DecisionKind) (string, int) {
	switch decision {
	case permissions.DecisionDeny:
		return "forbidden", 3
	case permissions.DecisionAsk:
		return "prompt", 2
	case permissions.DecisionAllow:
		return "allow", 1
	default:
		return string(decision), 0
	}
}

func loadFile(path string) (Set, error) {
	file, err := os.Open(path)
	if err != nil {
		return Set{}, err
	}
	defer file.Close()
	var set Set
	scanner := bufio.NewScanner(file)
	var statement strings.Builder
	statementLine, depth := 0, 0
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line, err := cleanPolicyLine(scanner.Text(), &depth)
		if err != nil {
			return Set{}, fmt.Errorf("%s:%d: %w", path, lineNumber, err)
		}
		if line == "" {
			continue
		}
		if statement.Len() == 0 {
			statementLine = lineNumber
		} else {
			statement.WriteByte(' ')
		}
		statement.WriteString(line)
		if depth > 0 {
			continue
		}
		loaded, err := parsePolicyStatement(statement.String(), fmt.Sprintf("%s:%d", path, statementLine))
		if err != nil {
			return Set{}, fmt.Errorf("%s:%d: %w", path, statementLine, err)
		}
		set.merge(loaded)
		statement.Reset()
	}
	if err := scanner.Err(); err != nil {
		return Set{}, err
	}
	if depth != 0 || statement.Len() > 0 {
		return Set{}, fmt.Errorf("%s:%d: unterminated policy statement", path, statementLine)
	}
	return set, nil
}

func cleanPolicyLine(line string, depth *int) (string, error) {
	var out strings.Builder
	quote, escaped := rune(0), false
	for _, ch := range line {
		if escaped {
			out.WriteRune(ch)
			escaped = false
			continue
		}
		if quote != 0 {
			out.WriteRune(ch)
			if ch == '\\' {
				escaped = true
			} else if ch == quote {
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
			out.WriteRune(ch)
		case '#':
			return strings.TrimSpace(out.String()), nil
		case '(':
			*depth++
			out.WriteRune(ch)
		case ')':
			*depth--
			if *depth < 0 {
				return "", fmt.Errorf("unexpected closing parenthesis")
			}
			out.WriteRune(ch)
		default:
			out.WriteRune(ch)
		}
	}
	if quote != 0 || escaped {
		return "", fmt.Errorf("unterminated string")
	}
	return strings.TrimSpace(out.String()), nil
}

func parsePolicyStatement(statement, source string) (Set, error) {
	switch {
	case strings.HasPrefix(statement, "prefix_rule("):
		rule, err := parseRule(statement)
		if err != nil {
			return Set{}, err
		}
		rule.Source = source
		return Set{Rules: []Rule{rule}}, nil
	case strings.HasPrefix(statement, "host_executable("):
		name, paths, err := parseHostExecutable(statement)
		if err != nil {
			return Set{}, err
		}
		return Set{HostExecutables: map[string][]string{name: paths}}, nil
	default:
		return Set{}, fmt.Errorf("expected prefix_rule(...) or host_executable(...)")
	}
}

func parseHostExecutable(statement string) (string, []string, error) {
	if !strings.HasSuffix(statement, ")") {
		return "", nil, fmt.Errorf("expected host_executable(...)")
	}
	name, err := extractStringArgument(statement, "name")
	if err != nil {
		return "", nil, err
	}
	key := executableLookupKey(name)
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return "", nil, fmt.Errorf("host_executable name must be a bare executable name (got %q)", name)
	}
	array, err := extractArrayArgument(statement, "paths")
	if err != nil {
		return "", nil, err
	}
	var rawPaths []string
	if err := json.Unmarshal([]byte(array), &rawPaths); err != nil {
		return "", nil, fmt.Errorf("host_executable paths must be a string array")
	}
	paths := make([]string, 0, len(rawPaths))
	for _, path := range rawPaths {
		if !filepath.IsAbs(path) {
			return "", nil, fmt.Errorf("host_executable paths must be absolute (got %s)", path)
		}
		path = filepath.Clean(path)
		if executableLookupKey(filepath.Base(path)) != key {
			return "", nil, fmt.Errorf("host_executable path %q must have basename %q", path, name)
		}
		if !containsString(paths, path) {
			paths = append(paths, path)
		}
	}
	return key, paths, nil
}

func parseRule(line string) (Rule, error) {
	if !strings.HasPrefix(line, "prefix_rule(") || !strings.HasSuffix(line, ")") {
		return Rule{}, fmt.Errorf("expected prefix_rule(...)")
	}
	array, err := extractPatternArray(line)
	if err != nil {
		return Rule{}, err
	}
	var pattern []string
	if err := json.Unmarshal([]byte(array), &pattern); err != nil || len(pattern) == 0 {
		return Rule{}, fmt.Errorf("pattern must be a non-empty string array")
	}
	decisionText, err := extractStringArgument(line, "decision")
	if err != nil {
		return Rule{}, err
	}
	var decision permissions.DecisionKind
	switch decisionText {
	case "allow":
		decision = permissions.DecisionAllow
	case "prompt", "ask":
		decision = permissions.DecisionAsk
	case "forbid", "forbidden", "deny":
		decision = permissions.DecisionDeny
	default:
		return Rule{}, fmt.Errorf("invalid rule decision %q", decisionText)
	}
	return Rule{Pattern: pattern, Decision: decision}, nil
}

func extractPatternArray(line string) (string, error) {
	return extractArrayArgument(line, "pattern")
}

func extractArrayArgument(line, name string) (string, error) {
	location := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=`).FindStringIndex(line)
	if location == nil {
		return "", fmt.Errorf("rule %s is required", name)
	}
	index := location[1]
	if index < 0 {
		return "", fmt.Errorf("rule %s is required", name)
	}
	start := strings.Index(line[index:], "[")
	if start < 0 {
		return "", fmt.Errorf("rule %s array is required", name)
	}
	start += index
	inString, escaped := false, false
	for i := start + 1; i < len(line); i++ {
		ch := line[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		if ch == '"' {
			inString = true
		} else if ch == ']' {
			return line[start : i+1], nil
		}
	}
	return "", fmt.Errorf("unterminated rule %s", name)
}

func extractStringArgument(line, name string) (string, error) {
	location := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*=`).FindStringIndex(line)
	if location == nil {
		return "", fmt.Errorf("rule %s is required", name)
	}
	rest := strings.TrimSpace(line[location[1]:])
	if !strings.HasPrefix(rest, `"`) {
		return "", fmt.Errorf("rule %s must be a string", name)
	}
	for i := 1; i < len(rest); i++ {
		if rest[i] == '"' && rest[i-1] != '\\' {
			value, err := strconv.Unquote(rest[:i+1])
			return value, err
		}
	}
	return "", fmt.Errorf("unterminated rule %s", name)
}

// Policy 在基础审批策略前应用命令前缀规则。
type Policy struct {
	Base permissions.Policy
	Set  Set
}

func (p Policy) Check(ctx context.Context, req permissions.Request) (permissions.Decision, error) {
	if err := ctx.Err(); err != nil {
		return permissions.Decision{}, err
	}
	base := p.Base
	if base == nil {
		base = permissions.AutoPolicy{}
	}
	if req.Action != permissions.ActionShell || len(p.Set.Rules) == 0 {
		return base.Check(ctx, req)
	}
	commands, dynamic := tokenizeCommands(req.Target)
	if len(commands) == 0 {
		return base.Check(ctx, req)
	}
	allAllowed := !dynamic
	prompt := false
	for _, command := range commands {
		allowed := false
		for _, matched := range p.Set.matches(command, true) {
			rule := matched.rule
			switch rule.Decision {
			case permissions.DecisionDeny:
				return permissions.Decision{Kind: permissions.DecisionDeny, Reason: "command forbidden by " + rule.Source}, nil
			case permissions.DecisionAsk:
				prompt = true
			case permissions.DecisionAllow:
				allowed = true
			}
		}
		allAllowed = allAllowed && allowed
	}
	if prompt {
		return permissions.Decision{Kind: permissions.DecisionAsk, Reason: "command requires confirmation by exec policy"}, nil
	}
	if allAllowed {
		return permissions.Decision{Kind: permissions.DecisionAllow, Reason: "command allowed by exec policy"}, nil
	}
	return base.Check(ctx, req)
}

func hasPrefix(command, pattern []string) bool {
	if len(command) < len(pattern) {
		return false
	}
	for i := range pattern {
		if command[i] != pattern[i] {
			return false
		}
	}
	return true
}

func (s Set) matches(command []string, resolveHostExecutables bool) []ruleMatch {
	exact := s.matchExact(command, "")
	if len(exact) > 0 || !resolveHostExecutables || len(command) == 0 || !filepath.IsAbs(command[0]) {
		return exact
	}
	program := filepath.Clean(command[0])
	basename := executableLookupKey(filepath.Base(program))
	if basename == "" {
		return nil
	}
	if paths, constrained := s.HostExecutables[basename]; constrained && !containsString(paths, program) {
		return nil
	}
	resolved := append([]string{basename}, command[1:]...)
	return s.matchExact(resolved, program)
}

func (s Set) matchExact(command []string, resolvedProgram string) []ruleMatch {
	var matches []ruleMatch
	for _, rule := range s.Rules {
		if hasPrefix(command, rule.Pattern) {
			matches = append(matches, ruleMatch{rule: rule, resolvedProgram: resolvedProgram})
		}
	}
	return matches
}

func executableLookupKey(name string) string {
	if runtime.GOOS != "windows" {
		return name
	}
	name = strings.ToLower(name)
	for _, suffix := range []string{".exe", ".cmd", ".bat", ".com"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func tokenizeCommands(input string) ([][]string, bool) {
	var commands [][]string
	var fields []string
	var token strings.Builder
	quote := rune(0)
	escaped, dynamic := false, false
	flushToken := func() {
		if token.Len() > 0 {
			fields = append(fields, token.String())
			token.Reset()
		}
	}
	flushCommand := func() {
		flushToken()
		if len(fields) > 0 {
			commands = append(commands, fields)
			fields = nil
		}
	}
	for _, ch := range input {
		if escaped {
			token.WriteRune(ch)
			escaped = false
			continue
		}
		if quote != '\'' && ch == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if ch == quote {
				quote = 0
			} else {
				if quote == '"' && (ch == '$' || ch == '`') {
					dynamic = true
				}
				token.WriteRune(ch)
			}
			continue
		}
		switch ch {
		case '\'', '"':
			quote = ch
		case ';', '|', '&', '\n', '(', ')':
			flushCommand()
		case '<', '>':
			dynamic = true
			flushCommand()
		case '$', '`':
			dynamic = true
			token.WriteRune(ch)
		default:
			if unicode.IsSpace(ch) {
				flushToken()
			} else {
				token.WriteRune(ch)
			}
		}
	}
	if escaped || quote != 0 {
		dynamic = true
	}
	flushCommand()
	for _, command := range commands {
		if invokesShellScript(command) {
			dynamic = true
			break
		}
	}
	return commands, dynamic
}

func invokesShellScript(command []string) bool {
	if len(command) < 2 {
		return false
	}
	switch filepath.Base(command[0]) {
	case "sh", "bash", "dash", "ksh", "zsh":
	default:
		return false
	}
	for _, argument := range command[1:] {
		if argument == "-c" || strings.HasPrefix(argument, "-") && strings.Contains(argument[1:], "c") {
			return true
		}
	}
	return false
}

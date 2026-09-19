// Package config loads PirateBox configuration.
//
// Luffy keeps the original shell-style piratebox.conf format so that
// existing installations can switch to the Go binary without rewriting
// their configuration. The parser understands:
//
//	KEY=value
//	KEY="value with $VARIABLES"
//	KEY='literal value'
//	export KEY=value
//	. /path/to/other.conf
//	source /path/to/other.conf
//
// Variables are expanded from previously assigned keys. Command
// substitution is limited to `$(cat file)` and `$(<file)`; other
// commands expand to the empty string and produce a warning.
package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Values holds raw configuration keys as parsed from shell-style files.
type Values map[string]string

// Get returns the value for key or the empty string.
func (v Values) Get(key string) string { return v[key] }

// GetDefault returns the value for key, or def when the key is absent.
func (v Values) GetDefault(key, def string) string {
	if val, ok := v[key]; ok && strings.TrimSpace(val) != "" {
		return val
	}
	return def
}

// Yes reports whether key is set to "yes" (case sensitive, matching the
// original shell comparisons).
func (v Values) Yes(key string) bool { return v[key] == "yes" }

// Parser reads shell-style configuration files.
type Parser struct {
	// BaseDir is used to resolve relative include paths.
	BaseDir string
	// MaxDepth limits include nesting to protect against cycles.
	MaxDepth int
	// Warnings collects non-fatal problems (missing includes, unsupported
	// command substitution).
	Warnings []string
}

// NewParser returns a parser with default settings.
func NewParser(baseDir string) *Parser {
	return &Parser{BaseDir: baseDir, MaxDepth: 16}
}

// ParseFile loads path and all files it includes.
func (p *Parser) ParseFile(path string) (Values, error) {
	values := Values{}
	if err := p.parseFile(path, values, 0, map[string]bool{}); err != nil {
		return nil, err
	}
	return values, nil
}

// ParseReader loads configuration from r. Includes resolve against
// p.BaseDir.
func (p *Parser) ParseReader(name string, r io.Reader) (Values, error) {
	values := Values{}
	if err := p.parseReader(name, r, values, 0, map[string]bool{}); err != nil {
		return nil, err
	}
	return values, nil
}

func (p *Parser) parseFile(path string, values Values, depth int, seen map[string]bool) error {
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.BaseDir, path)
	}
	clean := filepath.Clean(path)
	if seen[clean] {
		p.warnf("include cycle skipped: %s", clean)
		return nil
	}
	if depth > p.MaxDepth {
		return fmt.Errorf("config: include depth exceeded at %s", clean)
	}
	f, err := os.Open(clean)
	if err != nil {
		return fmt.Errorf("config: open %s: %w", clean, err)
	}
	defer f.Close()
	seen[clean] = true
	defer delete(seen, clean)
	return p.parseReader(clean, f, values, depth, seen)
}

func (p *Parser) parseReader(name string, r io.Reader, values Values, depth int, seen map[string]bool) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	dir := filepath.Dir(name)
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if inc, ok := parseInclude(line); ok {
			target := expand(inc, values, p)
			if !filepath.IsAbs(target) {
				target = filepath.Join(dir, target)
			}
			if _, err := os.Stat(target); err != nil {
				p.warnf("%s:%d: include not found: %s", name, lineNo, target)
				continue
			}
			if err := p.parseFile(target, values, depth+1, seen); err != nil {
				return err
			}
			continue
		}
		key, raw, ok := splitAssignment(line)
		if !ok {
			p.warnf("%s:%d: not an assignment, skipped: %s", name, lineNo, line)
			continue
		}
		if !validKey(key) {
			p.warnf("%s:%d: invalid key %q, skipped", name, lineNo, key)
			continue
		}
		values[key] = p.parseValue(raw, values)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("config: read %s: %w", name, err)
	}
	return nil
}

// parseInclude recognises `. path` and `source path` lines. A bare `.` or
// `source` with no path is not an include; the caller warns and skips it.
func parseInclude(line string) (string, bool) {
	for _, prefix := range []string{".", "source"} {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		rest := line[len(prefix):]
		if rest == "" || (rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		rest = strings.TrimSpace(rest)
		rest = strings.Trim(rest, `"'`)
		if rest == "" {
			continue
		}
		return rest, true
	}
	return "", false
}

func splitAssignment(line string) (key, raw string, ok bool) {
	line = strings.TrimPrefix(line, "export ")
	idx := strings.IndexByte(line, '=')
	if idx <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	return key, strings.TrimSpace(line[idx+1:]), true
}

func validKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// parseValue handles quoting and expansion of the right hand side of an
// assignment.
func (p *Parser) parseValue(raw string, values Values) string {
	if raw == "" {
		return ""
	}
	switch raw[0] {
	case '\'':
		if end := strings.IndexByte(raw[1:], '\''); end >= 0 {
			return raw[1 : 1+end]
		}
		return raw[1:]
	case '"':
		body, ok := quotedBody(raw[1:])
		if !ok {
			return expand(raw[1:], values, p)
		}
		return p.expandCommandSubst(expand(body, values, p), values)
	default:
		unquoted := cutComment(raw)
		return p.expandCommandSubst(expand(unquoted, values, p), values)
	}
}

// quotedBody returns the contents up to the closing double quote.
func quotedBody(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '\\', '"', '\'', '$', '`':
				b.WriteByte('\\')
				b.WriteByte(next)
				i++
				continue
			}
		}
		if c == '"' {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return b.String(), false
}

// cutComment removes an unquoted trailing comment from an unquoted value.
func cutComment(s string) string {
	for i := 1; i < len(s); i++ {
		if s[i] == '#' && (s[i-1] == ' ' || s[i-1] == '\t') {
			return strings.TrimRight(s[:i], " \t")
		}
	}
	return strings.TrimRight(s, " \t")
}

func (p *Parser) expandCommandSubst(s string, values Values) string {
	start := strings.Index(s, "$(")
	if start < 0 {
		return s
	}
	var b strings.Builder
	for {
		start = strings.Index(s, "$(")
		if start < 0 {
			b.WriteString(s)
			break
		}
		depth := 0
		end := -1
		for i := start + 1; i < len(s); i++ {
			switch s[i] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = i
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:start])
		b.WriteString(p.runCommandSubst(strings.TrimSpace(s[start+2:end]), values))
		s = s[end+1:]
	}
	return b.String()
}

func (p *Parser) runCommandSubst(cmd string, values Values) string {
	if strings.HasPrefix(cmd, "<") {
		return strings.TrimSpace(p.readSubstFile(strings.TrimSpace(cmd[1:]), values))
	}
	fields := strings.Fields(cmd)
	if len(fields) == 2 && fields[0] == "cat" {
		return strings.TrimSpace(p.readSubstFile(fields[1], values))
	}
	p.warnf("unsupported command substitution: $(%s)", cmd)
	return ""
}

func (p *Parser) readSubstFile(path string, values Values) string {
	path = expand(path, values, p)
	if !filepath.IsAbs(path) {
		path = filepath.Join(p.BaseDir, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		p.warnf("command substitution could not read %s: %v", path, err)
		return ""
	}
	return string(data)
}

func (p *Parser) warnf(format string, args ...any) {
	p.Warnings = append(p.Warnings, fmt.Sprintf(format, args...))
}

func isVarStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isVarChar(c byte) bool {
	return isVarStart(c) || (c >= '0' && c <= '9')
}

// expand substitutes $VAR and ${VAR} references. Backslash escapes special
// characters.
func expand(s string, values Values, p *Parser) string {
	if !strings.ContainsRune(s, '$') && !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			next := s[i+1]
			switch next {
			case '$', '"', '\'', '\\', '`':
				b.WriteByte(next)
				i++
				continue
			}
			b.WriteByte(c)
			continue
		}
		if c != '$' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			name := s[i+2 : i+2+end]
			b.WriteString(values[name])
			i += 2 + end
			continue
		}
		j := i + 1
		for j < len(s) && isVarChar(s[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		b.WriteString(values[s[i+1:j]])
		i = j - 1
	}
	return b.String()
}

// resolveReferences expands $VAR references in every value, using only
// names that are present in the map. It runs to a fixed point so a chain of
// defaults resolves fully, and it leaves unknown names untouched so an
// unset variable never silently erases part of a value.
//
// Values read from a configuration file are already expanded by the parser;
// this pass exists for the built-in defaults, which are stored in the same
// shell syntax (for example WWW_FOLDER="$PIRATEBOX_FOLDER/www").
func resolveReferences(values Values) Values {
	const maxPasses = 10 // far deeper than any real configuration
	out := make(Values, len(values))
	for k, v := range values {
		out[k] = v
	}
	for pass := 0; pass < maxPasses; pass++ {
		changed := false
		for k, v := range out {
			resolved := expandKnown(v, out)
			if resolved != v {
				out[k] = resolved
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	return out
}

// expandKnown substitutes $VAR and ${VAR} only when the name is defined.
func expandKnown(s string, values Values) string {
	if !strings.ContainsRune(s, '$') {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' {
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				b.WriteByte(c)
				continue
			}
			name := s[i+2 : i+2+end]
			if replacement, ok := values[name]; ok {
				b.WriteString(replacement)
				i += 2 + end
				continue
			}
			b.WriteByte(c)
			continue
		}
		j := i + 1
		for j < len(s) && isVarChar(s[j]) {
			j++
		}
		if j == i+1 {
			b.WriteByte(c)
			continue
		}
		if replacement, ok := values[s[i+1:j]]; ok {
			b.WriteString(replacement)
			i = j - 1
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// ParseDestinations parses the GLOBAL_DEST style list, e.g.
//
//	[ #255.255.255.255# , #10.255.255.255# , #192.168.77.255# ]
//
// into individual addresses. Plain single addresses are accepted too.
func ParseDestinations(raw string) []string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		token = strings.Trim(token, `"'#`)
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		out = append(out, token)
	}
	return out
}

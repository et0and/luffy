package forum

import (
	"fmt"
	"strings"
)

// htmlEscapeTable is copied from forest.py, including its quirks: some
// replacements are missing their closing semicolons and '{' maps to a digit
// entity. Existing boards store data escaped with this table, so the port
// keeps it byte for byte.
var htmlEscapeTable = map[rune]string{
	'&':  "&amp;",
	'"':  "&quot;",
	'\'': "&apos;",
	'>':  "&gt;",
	'<':  "&lt;",
	';':  "&#59;",
	'/':  "&#47;",
	'=':  "&#61;",
	':':  "&#58;",
	'?':  "&#63;",
	'!':  "&#33;",
	'(':  "&#40;",
	'{':  "&#121;",
	'[':  "&#91",
	'-':  "&#45",
}

var escaper = newReplacer(htmlEscapeTable)

func newReplacer(table map[rune]string) *strings.Replacer {
	pairs := make([]string, 0, len(table)*2)
	for r, replacement := range table {
		pairs = append(pairs, string(r), replacement)
	}
	return strings.NewReplacer(pairs...)
}

// StripHTML replaces the characters in htmlEscapeTable with their entities,
// exactly like forest.strip_html.
func StripHTML(text string) string { return escaper.Replace(text) }

// truncateRunes shortens s to at most n characters, matching Python string
// slicing.
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// ProcessAuthor cleans an author name, matching forest.process_author.
func ProcessAuthor(author string, maxAuthorLen int) string {
	trimmed := StripHTML(truncateRunes(author, maxAuthorLen))
	return strings.ReplaceAll(trimmed, "\t", " ")
}

// ProcessSubject cleans a subject, matching forest.process_subject.
func ProcessSubject(subject *string, maxSubjectLen int) string {
	if subject == nil {
		return "No Subject"
	}
	return truncateRunes(*subject, maxSubjectLen)
}

// ProcessBody cleans a message body and stores it in Python 2
// string_escape format, matching forest.process_body.
func ProcessBody(body string, maxBodyLen int) string {
	stripped := StripHTML(truncateRunes(body, maxBodyLen))
	withBreaks := strings.ReplaceAll(stripped, "\n", "<br/>\n")
	return StringEscape(withBreaks)
}

// StringEscape encodes a string like Python 2's 'string_escape' codec.
// Printable ASCII passes through, control characters become \xNN, and other
// runes are encoded as \uXXXX so decoding is lossless.
func StringEscape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r >= 0x20 && r < 0x7f {
				b.WriteRune(r)
				continue
			}
			// Python 2 saw UTF-8 byte strings, so non-ASCII characters
			// were escaped one byte at a time. Doing the same keeps
			// legacy boards and modern UTF-8 input round-trippable.
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, `\x%02x`, by)
			}
		}
	}
	return b.String()
}

// StringUnescape decodes Python 2 string_escape output. Unknown escapes are
// preserved as-is.
func StringUnescape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' || i+1 >= len(s) {
			b.WriteByte(c)
			continue
		}
		next := s[i+1]
		switch next {
		case '\\':
			b.WriteByte('\\')
			i++
		case '\'':
			b.WriteByte('\'')
			i++
		case '"':
			b.WriteByte('"')
			i++
		case 'n':
			b.WriteByte('\n')
			i++
		case 'r':
			b.WriteByte('\r')
			i++
		case 't':
			b.WriteByte('\t')
			i++
		case 'x':
			if i+3 < len(s) {
				if v, ok := hexByte(s[i+2 : i+4]); ok {
					b.WriteByte(byte(v))
					i += 3
					continue
				}
			}
			b.WriteByte(c)
		case 'u':
			if i+5 < len(s) {
				if v, ok := hexRune(s[i+2 : i+6]); ok {
					b.WriteRune(rune(v))
					i += 5
					continue
				}
			}
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func hexByte(s string) (int, bool) {
	if len(s) != 2 {
		return 0, false
	}
	hi, ok1 := hexVal(s[0])
	lo, ok2 := hexVal(s[1])
	if !ok1 || !ok2 {
		return 0, false
	}
	return hi<<4 | lo, true
}

func hexRune(s string) (int, bool) {
	if len(s) != 4 {
		return 0, false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		h, ok := hexVal(s[i])
		if !ok {
			return 0, false
		}
		v = v<<4 | h
	}
	return v, true
}

func hexVal(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, true
	}
	return 0, false
}

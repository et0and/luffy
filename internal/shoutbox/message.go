// Package shoutbox ports the PirateBox shoutbox.
//
// Original components:
//
//	python_lib/psogen.py     - message formatting, chat file handling
//	python_lib/messages.py   - wire format: piratebox;sb;01;<name>;<base64>
//	python_lib/broadcast.py  - UDP fan-out to the local network
//	python_lib/discover.py   - UDP listener for messages from other boxes
package shoutbox

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Wire fields, matching messages.py.
const (
	Magic        = "piratebox"
	TypeGeneral  = "gc"
	TypeShoutbox = "sb"
	Version      = "01"
	// Port is the UDP port used for global chat, from discover.py.
	Port = 12556
)

// Message is a PirateBox chat message in the original wire format.
type Message struct {
	Name    string
	Type    string
	decoded string
}

// NewMessage creates a general message with the given sender name. An empty
// name falls back to the machine hostname, matching messages.py.
func NewMessage(name string) *Message {
	if name == "" {
		name, _ = os.Hostname()
	}
	return &Message{Name: name, Type: TypeGeneral}
}

// NewShoutboxMessage creates a shoutbox message.
func NewShoutboxMessage(name string) *Message {
	msg := NewMessage(name)
	msg.Type = TypeShoutbox
	return msg
}

// Set encodes content and stores the wire representation.
func (m *Message) Set(content string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	if m.Type == "" {
		m.Type = TypeGeneral
	}
	if m.Name == "" {
		m.Name, _ = os.Hostname()
	}
	m.decoded = strings.Join([]string{Magic, m.Type, Version, m.Name, encoded}, ";")
}

// Wire returns the encoded message string.
func (m *Message) Wire() string { return m.decoded }

// Sender returns the message sender name.
func (m *Message) Sender() string { return m.Name }

// Content decodes the payload of the message.
func (m *Message) Content() (string, error) {
	parts := strings.Split(m.decoded, ";")
	if len(parts) < 5 {
		return "", fmt.Errorf("shoutbox: malformed message %q", m.decoded)
	}
	if parts[0] != Magic {
		return "", fmt.Errorf("shoutbox: not a piratebox message: %q", parts[0])
	}
	raw, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return "", fmt.Errorf("shoutbox: base64 payload: %w", err)
	}
	return string(raw), nil
}

// ErrNotPiratebox marks packets that are not PirateBox chat traffic.
var ErrNotPiratebox = errors.New("shoutbox: not a piratebox message")

// Parse decodes a wire message. It returns ErrNotPiratebox for foreign
// packets so the listener can ignore them silently.
func Parse(raw string) (*Message, error) {
	raw = strings.TrimSpace(raw)
	parts := strings.Split(raw, ";")
	if len(parts) < 5 || parts[0] != Magic {
		return nil, ErrNotPiratebox
	}
	msg := &Message{Type: parts[1], Name: parts[3], decoded: raw}
	if _, err := msg.Content(); err != nil {
		return nil, err
	}
	return msg, nil
}

// Line builds a chat line. When clientTime is set the browser supplied
// timestamp is used, otherwise now is used. This mirrors the
// SHOUTBOX_CLIENT_TIMESTAMP setting in psogen.py.
func Line(name, data, color, timestamp string, clientTime bool, now time.Time) string {
	at := now
	if clientTime {
		at = ParseUnix(timestamp, now)
	}
	return PrepareLine(name, data, color, at)
}

// PrepareLine formats one chat line, matching psogen.prepare_line.
//
// The exact HTML shape is kept because existing installations store chat
// history in this format.
func PrepareLine(name, data, color string, at time.Time) string {
	name = HTMLEscape(name)
	data = HTMLEscape(data)
	color = HTMLEscape(color)
	return fmt.Sprintf("<div class='message'><date>%s</date> <name>%s:</name> <data class='%s'>%s</data></div>\n",
		at.Format("15:04:05"), name, color, data)
}

// GeneratePage wraps content in the shoutbox HTML frame from psogen.py.
func GeneratePage(content string) string {
	var b strings.Builder
	b.WriteString("<html><head><meta http-equiv='cache-control' content='no-cache'>" +
		"<meta name='GENERATOR' content='PyShoutOut'><title>Shout-Out Data</title><body>")
	b.WriteString(content)
	b.WriteString("</body></html>")
	return b.String()
}

// HTMLEscape mirrors psogen.html_escape: only these four characters are
// replaced. Notably '&' is left alone, matching the original.
func HTMLEscape(text string) string {
	if !strings.ContainsAny(text, `"'><`) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch r {
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		case '>':
			b.WriteString("&gt;")
		case '<':
			b.WriteString("&lt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ClientTimestamp parses SHOUTBOX_CLIENT_TIMESTAMP style values.
func ClientTimestamp(value string) bool { return value == "yes" }

// ParseUnix converts a browser supplied Unix timestamp, falling back to now
// for malformed values.
func ParseUnix(raw string, now time.Time) time.Time {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return now
	}
	sec := int64(f)
	nsec := int64((f - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

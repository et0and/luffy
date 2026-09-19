package shoutbox

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMessageRoundTrip(t *testing.T) {
	msg := NewShoutboxMessage("alpha")
	msg.Set("<b>hello</b>")
	wire := msg.Wire()
	if !strings.HasPrefix(wire, "piratebox;sb;01;alpha;") {
		t.Fatalf("unexpected wire format: %q", wire)
	}
	parsed, err := Parse(wire)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	content, err := parsed.Content()
	if err != nil {
		t.Fatalf("content: %v", err)
	}
	if content != "<b>hello</b>" {
		t.Errorf("content = %q", content)
	}
	if parsed.Sender() != "alpha" {
		t.Errorf("sender = %q", parsed.Sender())
	}
	if parsed.Type != TypeShoutbox {
		t.Errorf("type = %q", parsed.Type)
	}
}

func TestMessageEmptyNameUsesHostname(t *testing.T) {
	msg := NewShoutboxMessage("")
	msg.Set("x")
	if msg.Sender() == "" {
		t.Error("expected hostname fallback")
	}
}

func TestParseRejectsForeignAndMalformed(t *testing.T) {
	for _, raw := range []string{
		"",
		"hello world",
		"piratebox",
		"piratebox;sb",
		"other;sb;01;name;aGVsbG8=",
		"piratebox;sb;01;name;!!!not-base64!!!",
	} {
		if _, err := Parse(raw); err == nil {
			t.Errorf("Parse(%q) should fail", raw)
		}
	}
}

func TestPrepareLineFormat(t *testing.T) {
	at := time.Date(2026, 9, 19, 21, 5, 9, 0, time.UTC)
	line := PrepareLine("Ana", "hello", "blue", at)
	want := "<div class='message'><date>21:05:09</date> <name>Ana:</name> <data class='blue'>hello</data></div>\n"
	if line != want {
		t.Errorf("line = %q, want %q", line, want)
	}
}

func TestPrepareLineEscaping(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	line := PrepareLine(`<script>"x"</script>`, `a > b & c 'q'`, "def", at)
	for _, bad := range []string{"<script>", `"x"`} {
		if strings.Contains(line, bad) {
			t.Errorf("line contains unescaped %q: %s", bad, line)
		}
	}
	if !strings.Contains(line, "&lt;script&gt;") {
		t.Errorf("angle brackets not escaped: %s", line)
	}
	if !strings.Contains(line, "&quot;x&quot;") {
		t.Errorf("quotes not escaped: %s", line)
	}
	// The original leaves ampersands alone.
	if !strings.Contains(line, "& c") {
		t.Errorf("ampersand should not be escaped: %s", line)
	}
}

func TestLineTimestampModes(t *testing.T) {
	// Client timestamps use the local timezone, as Python's
	// datetime.fromtimestamp does.
	now := time.Unix(1758283200, 0)
	server := Line("n", "d", "def", "0", false, now)
	if !strings.Contains(server, "<date>"+now.Format("15:04:05")+"</date>") {
		t.Errorf("server timestamp missing: %s", server)
	}
	clientAt := now.Add(-time.Hour)
	client := Line("n", "d", "def", strconv.FormatInt(clientAt.Unix(), 10), true, now)
	if !strings.Contains(client, "<date>"+clientAt.Format("15:04:05")+"</date>") {
		t.Errorf("client timestamp missing: %s", client)
	}
	bad := Line("n", "d", "def", "not-a-number", true, now)
	if !strings.Contains(bad, "<date>"+now.Format("15:04:05")+"</date>") {
		t.Errorf("bad timestamp should fall back to now: %s", bad)
	}
}

func TestGeneratePage(t *testing.T) {
	page := GeneratePage("<div>hi</div>")
	for _, want := range []string{"<html>", "PyShoutOut", "<div>hi</div>", "</body></html>"} {
		if !strings.Contains(page, want) {
			t.Errorf("page missing %q: %s", want, page)
		}
	}
}

func TestStorePrependAndReset(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "data.pso"))

	if got, err := store.Read(); err != nil || got != "" {
		t.Fatalf("missing file should read empty, got %q err %v", got, err)
	}
	if err := store.Reset("<div>init</div>\n"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := store.Prepend("<div>one</div>\n"); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	if _, err := store.Prepend("<div>two</div>\n"); err != nil {
		t.Fatalf("prepend: %v", err)
	}
	content, err := store.Read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := "<div>two</div>\n<div>one</div>\n<div>init</div>\n"
	if content != want {
		t.Errorf("content = %q, want %q", content, want)
	}
}

func TestStoreRejectsEmptyLine(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "data.pso"))
	if _, err := store.Prepend(""); err == nil {
		t.Error("expected error for empty line")
	}
}

func TestStoreGeneratePage(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "data.pso"))
	if _, err := store.Prepend("<div>newest</div>\n"); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "chat_content.html")
	if err := store.GeneratePage(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<div>newest</div>") || !strings.Contains(string(data), "PyShoutOut") {
		t.Errorf("generated page wrong: %s", data)
	}
}

type fakeSender struct {
	sent      []string
	failOn    string
	failCount int
}

func (f *fakeSender) Send(destination string, port int, payload []byte) error {
	if destination == f.failOn && f.failCount > 0 {
		f.failCount--
		return os.ErrDeadlineExceeded
	}
	f.sent = append(f.sent, destination+"|"+strconv.Itoa(port)+"|"+string(payload))
	return nil
}

func TestBroadcasterFansOut(t *testing.T) {
	fake := &fakeSender{}
	b := &Broadcaster{
		Destinations: []string{"255.255.255.255", "192.168.77.255", "ff02::1"},
		Sender:       fake,
	}
	if err := b.Send("payload"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(fake.sent) != 3 {
		t.Fatalf("sent %d packets, want 3: %v", len(fake.sent), fake.sent)
	}
	if fake.sent[0] != "255.255.255.255|12556|payload" {
		t.Errorf("packet = %q", fake.sent[0])
	}
}

func TestBroadcasterContinuesAfterError(t *testing.T) {
	fake := &fakeSender{failOn: "10.0.0.255", failCount: 1}
	b := &Broadcaster{
		Destinations: []string{"10.0.0.255", "192.168.1.255"},
		Sender:       fake,
	}
	if err := b.Send("x"); err == nil {
		t.Error("expected first error to be reported")
	}
	if len(fake.sent) != 1 || fake.sent[0] != "192.168.1.255|12556|x" {
		t.Errorf("remaining destination not attempted: %v", fake.sent)
	}
}

func TestBroadcasterNoDestinations(t *testing.T) {
	b := &Broadcaster{}
	if err := b.Send("x"); err == nil {
		t.Error("expected error without destinations")
	}
}

func TestUDPSenderRoundTrip(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp loopback: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port

	var sender UDPSender
	if err := sender.Send("127.0.0.1", port, []byte("hello-udp")); err != nil {
		t.Fatalf("send: %v", err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if string(buf[:n]) != "hello-udp" {
		t.Errorf("received %q", buf[:n])
	}
}

func TestListenerDropsDuplicatesAndForeignPackets(t *testing.T) {
	var received []string
	l := &Listener{Handler: func(content string) error {
		received = append(received, content)
		return nil
	}}
	msg := NewShoutboxMessage("box")
	msg.Set("<div>from other box</div>")
	l.Handle(msg.Wire())
	l.Handle(msg.Wire()) // duplicate: ignored
	l.Handle("something else entirely")
	l.Handle("piratebox;gc;01;x;aGk=") // general chat: ignored

	if len(received) != 1 {
		t.Fatalf("received %d messages, want 1: %v", len(received), received)
	}
	if received[0] != "<div>from other box</div>" {
		t.Errorf("content = %q", received[0])
	}
}

func TestListenerServesUDP(t *testing.T) {
	// Bind an ephemeral port; 12556 may be taken on developer machines.
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp loopback: %v", err)
	}
	addr := conn.LocalAddr().String()

	got := make(chan string, 1)
	l := &Listener{
		Handler: func(content string) error {
			select {
			case got <- content:
			default:
			}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- l.Serve(ctx, conn) }()

	msg := NewShoutboxMessage("sender")
	msg.Set("network hello")
	if err := (UDPSender{}).Send("127.0.0.1", portOf(t, addr), []byte(msg.Wire())); err != nil {
		t.Fatalf("send: %v", err)
	}

	select {
	case content := <-got:
		if content != "network hello" {
			t.Errorf("content = %q", content)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for UDP message")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("listener returned %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("listener did not stop on cancel")
	}
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestListenAndServeBindsAndStops(t *testing.T) {
	// Use an ephemeral port via Addr so the test never collides with a
	// running PirateBox on port 12556.
	probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp loopback: %v", err)
	}
	addr := probe.LocalAddr().String()
	probe.Close()

	got := make(chan string, 1)
	l := &Listener{
		Addr: addr,
		Handler: func(content string) error {
			select {
			case got <- content:
			default:
			}
			return nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.ListenAndServe(ctx) }()

	// The socket may need a moment to bind.
	deadline := time.Now().Add(3 * time.Second)
	msg := NewShoutboxMessage("sender")
	msg.Set("via ListenAndServe")
	var sendErr error
	for time.Now().Before(deadline) {
		sendErr = (UDPSender{}).Send("127.0.0.1", portOf(t, addr), []byte(msg.Wire()))
		if sendErr == nil {
			select {
			case <-got:
				goto received
			case <-time.After(50 * time.Millisecond):
			}
		}
	}
	t.Fatalf("no message received, last send error: %v", sendErr)
received:
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("listener returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listener did not stop")
	}
}

func TestListenAndServeBindFailure(t *testing.T) {
	l := &Listener{Addr: "256.256.256.256:1"}
	if err := l.ListenAndServe(context.Background()); err == nil {
		t.Error("expected bind error for invalid address")
	}
}

func TestStorePath(t *testing.T) {
	store := NewStore("/tmp/chat.pso")
	if store.Path() != "/tmp/chat.pso" {
		t.Errorf("Path = %q", store.Path())
	}
}

func TestGeneratePageCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "cgi-bin", "data.pso"))
	out := filepath.Join(dir, "nested", "www", "chat_content.html")
	if err := store.GeneratePage(out); err != nil {
		t.Fatalf("GeneratePage should create missing directories: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("generated page missing: %v", err)
	}
}

func TestListenerHandlerErrorIsLogged(t *testing.T) {
	var buf bytes.Buffer
	l := &Listener{
		Logger:  slog.New(slog.NewTextHandler(&buf, nil)),
		Handler: func(string) error { return os.ErrInvalid },
	}
	msg := NewShoutboxMessage("x")
	msg.Set("content")
	l.Handle(msg.Wire())
	if !strings.Contains(buf.String(), "handler failed") {
		t.Errorf("handler error not logged: %q", buf.String())
	}
}

func TestListenerMalformedPacketIsLogged(t *testing.T) {
	var buf bytes.Buffer
	l := &Listener{Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	// Valid prefix but invalid base64 payload.
	l.Handle("piratebox;sb;01;name;not!base64")
	if !strings.Contains(buf.String(), "malformed") {
		t.Errorf("malformed packet not logged: %q", buf.String())
	}
}

func TestListenerGeneralChatIsIgnored(t *testing.T) {
	var buf bytes.Buffer
	l := &Listener{Logger: slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))}
	msg := NewMessage("box")
	msg.Set("general")
	l.Handle(msg.Wire())
	if !strings.Contains(buf.String(), "non-shoutbox") {
		t.Errorf("general chat should be logged at debug: %q", buf.String())
	}
}

func TestListenerNilHandler(t *testing.T) {
	l := &Listener{}
	msg := NewShoutboxMessage("x")
	msg.Set("content")
	l.Handle(msg.Wire()) // must not panic
}

func TestBroadcasterUsesDefaultSenderAndPort(t *testing.T) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp loopback: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	b := &Broadcaster{Destinations: []string{"127.0.0.1"}, Port: port}
	if err := b.Send("default-sender"); err != nil {
		t.Fatalf("send: %v", err)
	}
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if string(buf[:n]) != "default-sender" {
		t.Errorf("received %q", buf[:n])
	}
}

func TestBroadcasterLogsFailures(t *testing.T) {
	var buf bytes.Buffer
	fake := &fakeSender{failOn: "10.0.0.1", failCount: 1}
	b := &Broadcaster{
		Destinations: []string{"10.0.0.1"},
		Sender:       fake,
		Logger:       slog.New(slog.NewTextHandler(&buf, nil)),
	}
	if err := b.Send("x"); err == nil {
		t.Error("expected error")
	}
	if !strings.Contains(buf.String(), "broadcast failed") {
		t.Errorf("failure not logged: %q", buf.String())
	}
}

func TestUDPSenderIPv6(t *testing.T) {
	conn, err := net.ListenPacket("udp6", "[::1]:0")
	if err != nil {
		t.Skipf("no ipv6 loopback: %v", err)
	}
	defer conn.Close()
	port := conn.LocalAddr().(*net.UDPAddr).Port
	if err := (UDPSender{}).Send("::1", port, []byte("v6")); err != nil {
		t.Fatalf("send: %v", err)
	}
	buf := make([]byte, 16)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("receive: %v", err)
	}
	if string(buf[:n]) != "v6" {
		t.Errorf("received %q", buf[:n])
	}
}

func TestUDPSenderBadAddress(t *testing.T) {
	if err := (UDPSender{}).Send("256.256.256.256", 1, []byte("x")); err == nil {
		t.Error("expected dial error")
	}
}

func TestMessageSetDefaultsTypeAndName(t *testing.T) {
	m := &Message{}
	m.Set("content")
	if m.Type != TypeGeneral {
		t.Errorf("type = %q", m.Type)
	}
	if m.Sender() == "" {
		t.Error("name should default to the hostname")
	}
	if _, err := m.Content(); err != nil {
		t.Errorf("content: %v", err)
	}
}

func TestMessageContentErrors(t *testing.T) {
	m := &Message{decoded: "piratebox;sb;01;name"}
	if _, err := m.Content(); err == nil {
		t.Error("short message should fail")
	}
	m = &Message{decoded: "other;sb;01;name;aGk="}
	if _, err := m.Content(); err == nil {
		t.Error("wrong magic should fail")
	}
}

func TestPrepareLinePlainTextFastPath(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	line := PrepareLine("plain", "no special chars", "def", at)
	if !strings.Contains(line, "no special chars") {
		t.Errorf("line = %q", line)
	}
}

func TestServeStopsCleanlyOnReadError(t *testing.T) {
	// A listener whose connection fails immediately must return an error and
	// leave no goroutine blocked on the context.
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no udp loopback: %v", err)
	}
	l := &Listener{}
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { done <- l.Serve(ctx, conn) }()
	conn.Close()
	select {
	case err := <-done:
		// Closing the socket from outside is reported as a read error or as
		// a clean stop; both are acceptable as long as it returns.
		_ = err
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after the connection closed")
	}
}

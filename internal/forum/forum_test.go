package forum

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := Defaults(dir)
	cfg.Now = func() time.Time { return time.Date(2026, 9, 19, 20, 30, 0, 0, time.UTC) }
	return New(cfg), dir
}

func TestStripHTMLMatchesForestTable(t *testing.T) {
	cases := map[string]string{
		"a&b":   "a&amp;b",
		`"q"`:   "&quot;q&quot;",
		"a>b<c": "a&gt;b&lt;c",
		"semi;": "semi&#59;",
		"a/b":   "a&#47;b",
		"a=b":   "a&#61;b",
		"a:b":   "a&#58;b",
		"a?b":   "a&#63;b",
		"a!b":   "a&#33;b",
		"(x)":   "&#40;x)",
		"{x}":   "&#121;x}",
		"[x]":   "&#91x]",
		"a-b":   "a&#45b",
		"plain": "plain",
	}
	for in, want := range cases {
		if got := StripHTML(in); got != want {
			t.Errorf("StripHTML(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStringEscapeMatchesPython(t *testing.T) {
	// Python 2: "a\nb\tc\\d".encode('string_escape') == 'a\\nb\\tc\\\\d'
	got := StringEscape("a\nb\tc\\d")
	want := `a\nb\tc\\d`
	if got != want {
		t.Errorf("StringEscape = %q, want %q", got, want)
	}
	// Quotes are not escaped by the string_escape codec.
	if got := StringEscape(`say "hi"`); got != `say "hi"` {
		t.Errorf("quotes should pass through, got %q", got)
	}
}

func TestStringEscapeRoundTrip(t *testing.T) {
	cases := []string{
		"plain ascii",
		"line1<br/>\nline2",
		"tab\there",
		"back\\slash",
		"unicode \u00e4\u4e2d\u6587",
		"control \x01\x02",
	}
	for _, in := range cases {
		encoded := StringEscape(in)
		if strings.Contains(encoded, "\n") {
			t.Errorf("encoded %q still contains a newline", encoded)
		}
		if decoded := StringUnescape(encoded); decoded != in {
			t.Errorf("round trip %q -> %q -> %q", in, encoded, decoded)
		}
	}
}

func TestStringUnescapeLegacyData(t *testing.T) {
	// A post stored by the Python version: <br/> followed by literal \n.
	stored := `hello<br/>\nworld`
	if got := StringUnescape(stored); got != "hello<br/>\nworld" {
		t.Errorf("legacy decode = %q", got)
	}
	if got := StringUnescape(`caf\xc3\xa9`); got != "café" {
		t.Errorf("byte escape decode = %q", got)
	}
}

func TestNewThreadWritesForestFormat(t *testing.T) {
	store, dir := fixedStore(t)
	hash, err := store.NewThread("Alice", "First post", "Hello world")
	if err != nil {
		t.Fatalf("new thread: %v", err)
	}
	if len(hash) != 32 {
		t.Errorf("hash = %q, want md5 hex", hash)
	}

	index, err := os.ReadFile(filepath.Join(dir, "index.txt"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	wantIndex := hash + "\t19 Sep 2026 20:30:00\t0\t-\tAlice\tFirst post"
	if strings.TrimSpace(string(index)) != wantIndex {
		t.Errorf("index = %q, want %q", index, wantIndex)
	}

	thread, err := os.ReadFile(filepath.Join(dir, hash))
	if err != nil {
		t.Fatalf("read thread: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(thread)), "\n")
	if len(lines) != 2 {
		t.Fatalf("thread lines = %d, want 2: %q", len(lines), thread)
	}
	if lines[0] != hash+"\tFirst post" {
		t.Errorf("thread header = %q", lines[0])
	}
	if lines[1] != "19 Sep 2026 20:30:00\tAlice\tHello world" {
		t.Errorf("post line = %q", lines[1])
	}
}

func TestNewThreadValidation(t *testing.T) {
	store, _ := fixedStore(t)
	if _, err := store.NewThread("a", "", "body"); err != ErrNoSubject {
		t.Errorf("empty subject err = %v", err)
	}
	if _, err := store.NewThread("a", "subject", ""); err != ErrNoBody {
		t.Errorf("empty body err = %v", err)
	}
}

func TestReplyUpdatesIndexAndMovesToTop(t *testing.T) {
	store, dir := fixedStore(t)
	first, err := store.NewThread("Alice", "thread one", "one")
	if err != nil {
		t.Fatal(err)
	}
	store.Config()
	// A second thread appears later in time.
	cfg := store.Config()
	cfg.Now = func() time.Time { return time.Date(2026, 9, 19, 21, 0, 0, 0, time.UTC) }
	second, err := New(cfg).NewThread("Bob", "thread two", "two")
	if err != nil {
		t.Fatal(err)
	}
	// But the store still writes the original directory.
	_ = second

	// Reply to the older thread; it must jump to the top of the index.
	if err := store.Reply(first, "Carol", "a reply"); err != nil {
		t.Fatalf("reply: %v", err)
	}
	index, _ := os.ReadFile(filepath.Join(dir, "index.txt"))
	lines := strings.Split(string(index), "\n")
	if len(lines) != 2 {
		t.Fatalf("index lines = %d: %q", len(lines), index)
	}
	top := strings.Split(lines[0], "\t")
	if top[0] != first || top[2] != "1" {
		t.Errorf("replied thread not first with reply count: %q", lines[0])
	}
	if top[3] != "19 Sep 2026 20:30:00" {
		t.Errorf("last reply = %q", top[3])
	}

	thread, err := store.Thread(first, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(thread.Posts) != 2 {
		t.Fatalf("posts = %d", len(thread.Posts))
	}
	if thread.Posts[1].Author != "Carol" || thread.Posts[1].Body != "a reply" {
		t.Errorf("second post = %+v", thread.Posts[1])
	}
}

func TestReplyValidation(t *testing.T) {
	store, _ := fixedStore(t)
	if err := store.Reply("doesnotexist", "a", "b"); err != ErrInvalidThread {
		t.Errorf("invalid thread err = %v", err)
	}
	hash, _ := store.NewThread("a", "s", "b")
	if err := store.Reply(hash, "a", ""); err != ErrNoBody {
		t.Errorf("empty body err = %v", err)
	}
}

func TestBodyEscapingAndLineBreaks(t *testing.T) {
	store, dir := fixedStore(t)
	hash, err := store.NewThread("a", "s", "line1\nline2 <b>bold</b>")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, hash))
	if !strings.Contains(string(raw), `<br/>\n`) {
		t.Errorf("stored body should use <br/>\\n: %q", raw)
	}
	if strings.Contains(string(raw), "<b>") {
		t.Errorf("stored body should be escaped: %q", raw)
	}
	thread, _ := store.Thread(hash, 0)
	if !strings.Contains(thread.Posts[0].Body, "<br/>\n") {
		t.Errorf("display body should decode line breaks: %q", thread.Posts[0].Body)
	}
	if !strings.Contains(thread.Posts[0].Body, "&lt;b&gt;") {
		t.Errorf("display body should keep entities: %q", thread.Posts[0].Body)
	}
}

func TestProcessBodyTruncation(t *testing.T) {
	long := strings.Repeat("x", 10050)
	out := ProcessBody(long, 10000)
	if len(out) != 10000 {
		t.Errorf("truncated length = %d, want 10000", len(out))
	}
}

func TestValidHashRejectsTraversal(t *testing.T) {
	store, _ := fixedStore(t)
	for _, bad := range []string{"", "../index", "a/b", "a.b", "a b"} {
		if store.ValidHash(bad) {
			t.Errorf("ValidHash(%q) should be false", bad)
		}
	}
}

func TestListThreadsPaging(t *testing.T) {
	store, dir := fixedStore(t)
	cfg := store.Config()
	cfg.IndexPageSize = 2
	cfg.Now = func() time.Time { return time.Date(2026, 9, 19, 20, 0, 0, 0, time.UTC) }
	store = New(cfg)
	for i := 0; i < 5; i++ {
		if _, err := store.NewThread("a", "subject", "body"); err != nil {
			t.Fatal(err)
		}
	}
	page1, total, err := store.ListThreads(0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(page1) != 2 {
		t.Errorf("total=%d len=%d", total, len(page1))
	}
	page3, _, _ := store.ListThreads(4)
	if len(page3) != 1 {
		t.Errorf("last page len = %d", len(page3))
	}
	empty, _, _ := store.ListThreads(100)
	if len(empty) != 0 {
		t.Errorf("overflow page len = %d", len(empty))
	}
	_ = dir
}

func TestLegacyBoardCompatibility(t *testing.T) {
	dir := t.TempDir()
	// Files exactly as written by forest.py.
	hash := "1c2e3d4f5a6b7c8d9e0f1a2b3c4d5e6f"
	index := hash + "\t19 Sep 2026 20:30:00\t1\t19 Sep 2026 20:31:00\tAlice\tLegacy thread"
	thread := hash + "\tLegacy thread\n" +
		"19 Sep 2026 20:30:00\tAlice\thello<br/>\\nworld\n" +
		"19 Sep 2026 20:31:00\tBob\t%3Cb%3Eescaped%3C%2Fb%3E\n"
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, hash), []byte(thread), 0o644); err != nil {
		t.Fatal(err)
	}
	store := New(Defaults(dir))
	summaries, total, err := store.ListThreads(0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || summaries[0].Subject != "Legacy thread" || summaries[0].Replies != "1" {
		t.Errorf("legacy summary = %+v total=%d", summaries, total)
	}
	got, err := store.Thread(hash, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Subject != "Legacy thread" {
		t.Errorf("subject = %q", got.Subject)
	}
	if len(got.Posts) != 2 {
		t.Fatalf("posts = %d", len(got.Posts))
	}
	if !strings.Contains(got.Posts[0].Body, "<br/>\n") {
		t.Errorf("legacy body not decoded: %q", got.Posts[0].Body)
	}

	// Replying to the legacy thread must keep the file format.
	if err := store.Reply(hash, "Carol", "new reply"); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(filepath.Join(dir, hash))
	if !strings.Contains(string(updated), "Carol\tnew reply") {
		t.Errorf("reply not appended: %q", updated)
	}
	indexData, _ := os.ReadFile(filepath.Join(dir, "index.txt"))
	if !strings.Contains(string(indexData), "\t2\t") {
		t.Errorf("reply count not updated: %q", indexData)
	}
}

func TestRenderIndexContainsThreads(t *testing.T) {
	store, _ := fixedStore(t)
	if _, err := store.NewThread("Alice", "Hello board", "body"); err != nil {
		t.Fatal(err)
	}
	summaries, total, _ := store.ListThreads(0)
	page := store.RenderIndex(summaries, total, 0)
	for _, want := range []string{
		"Luffy Board", "Hello board", "Alice", "Start a new thread",
		"/content/css/forest.css", "threads_table",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("index page missing %q", want)
		}
	}
}

func TestRenderThreadContainsPostsAndForms(t *testing.T) {
	store, _ := fixedStore(t)
	hash, _ := store.NewThread("Alice", "Subject here", "first body")
	thread, _ := store.Thread(hash, 0)
	page := store.RenderThread(thread, 0)
	for _, want := range []string{"Subject here", "first body", "Reply to this thread", "?new=reply&amp;thread=" + hash, "&lt;&lt; Main"} {
		if !strings.Contains(page, want) {
			t.Errorf("thread page missing %q", want)
		}
	}
}

func TestRenderPagingLinks(t *testing.T) {
	store, _ := fixedStore(t)
	links := store.RenderPagingLinks(0, 45, 20, "")
	if !strings.Contains(links, "1") || !strings.Contains(links, `<a href="?offset=20">2</a>`) || !strings.Contains(links, `<a href="?offset=40">3</a>`) {
		t.Errorf("index paging links = %q", links)
	}
	if got := store.RenderPagingLinks(0, 5, 20, ""); got != "" {
		t.Errorf("single page should have no links, got %q", got)
	}
	threadLinks := store.RenderPagingLinks(0, 45, 20, "abc")
	if !strings.Contains(threadLinks, "thread=abc") {
		t.Errorf("thread paging links = %q", threadLinks)
	}
}

func TestHandlerLifecycle(t *testing.T) {
	store, _ := fixedStore(t)
	handler := NewHandler(store)

	// Create a thread.
	form := url.Values{"author": {"Alice"}, "subject": {"Hi"}, "body": {"Hello"}}
	req := httptest.NewRequest(http.MethodPost, "/board/?new=thread", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("new thread status = %d, body %s", rec.Code, rec.Body.String())
	}
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "?thread=") {
		t.Fatalf("redirect location = %q", location)
	}
	hash := strings.TrimPrefix(location, "?thread=")

	// Index lists it.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/", nil))
	if !strings.Contains(rec.Body.String(), "Hi") {
		t.Errorf("index missing thread: %s", rec.Body.String())
	}

	// Thread page.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/?thread="+hash, nil))
	if !strings.Contains(rec.Body.String(), "Hello") {
		t.Errorf("thread missing post: %s", rec.Body.String())
	}

	// Reply.
	form = url.Values{"author": {"Bob"}, "body": {"A reply"}}
	req = httptest.NewRequest(http.MethodPost, "/board/?new=reply&thread="+hash, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("reply status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/?thread="+hash, nil))
	if !strings.Contains(rec.Body.String(), "A reply") {
		t.Errorf("reply missing from thread: %s", rec.Body.String())
	}

	// Invalid thread shows the error but not a 500.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/?thread=missing", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid Thread Specified") {
		t.Errorf("invalid thread response = %d %s", rec.Code, rec.Body.String())
	}

	// Posting without a subject reports the forest error.
	form = url.Values{"author": {"x"}, "body": {"y"}}
	req = httptest.NewRequest(http.MethodPost, "/board/?new=thread", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "No Subject Given") {
		t.Errorf("missing subject response = %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerOffsetParsing(t *testing.T) {
	for in, want := range map[string]int{"": 0, "20": 20, "abc": 0, "-5": 0, "2e1": 0} {
		if got := parseOffset(in); got != want {
			t.Errorf("parseOffset(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestConcurrentPosting(t *testing.T) {
	store, dir := fixedStore(t)
	hash, err := store.NewThread("a", "concurrent", "start")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			done <- store.Reply(hash, "bot", "spam")
		}()
	}
	for i := 0; i < 8; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent reply: %v", err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "index.txt"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(string(data)), "\t")
	if len(parts) != 6 || parts[2] != "8" {
		t.Errorf("reply count after concurrency = %q", parts)
	}
}

func TestStringUnescapeAllEscapes(t *testing.T) {
	cases := map[string]string{
		`\n`:         "\n",
		`\r`:         "\r",
		`\t`:         "\t",
		`\\`:         `\`,
		`\'`:         "'",
		`\"`:         `"`,
		`\x41`:       "A",
		`\u4e2d`:     "中",
		`\U0001F600`: "😀",
		`\q`:         `\q`,
		`\x`:         `\x`,
		`\xZZ`:       `\xZZ`,
		`\u12`:       `\u12`,
		`\uZZZZ`:     `\uZZZZ`,
		`trailing\`:  `trailing\`,
	}
	for in, want := range cases {
		if got := StringUnescape(in); got != want {
			t.Errorf("StringUnescape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStringEscapeControlCharacters(t *testing.T) {
	if got := StringEscape("\x00\x1f\x7f"); got != `\x00\x1f\x7f` {
		t.Errorf("control chars = %q", got)
	}
	if got := StringEscape("plain"); got != "plain" {
		t.Errorf("plain = %q", got)
	}
}

func TestProcessAuthorAndSubjectHelpers(t *testing.T) {
	if got := ProcessAuthor("a\tb", 20); got != "a b" {
		t.Errorf("tab not replaced: %q", got)
	}
	if got := ProcessAuthor(strings.Repeat("x", 30), 20); len(got) != 20 {
		t.Errorf("author length = %d", len(got))
	}
	if got := ProcessSubject(nil, 100); got != "No Subject" {
		t.Errorf("nil subject = %q", got)
	}
	subject := strings.Repeat("y", 150)
	if got := ProcessSubject(&subject, 100); len(got) != 100 {
		t.Errorf("subject length = %d", len(got))
	}
}

func TestRenderStandaloneError(t *testing.T) {
	store, _ := fixedStore(t)
	page := store.RenderStandaloneError("boom")
	if !strings.Contains(page, "boom") || !strings.Contains(page, "Luffy Board") {
		t.Errorf("standalone error page = %s", page)
	}
}

func TestRenderRedirect(t *testing.T) {
	store, _ := fixedStore(t)
	page := store.RenderRedirect("abc", 20)
	if !strings.Contains(page, "/board/?thread=abc&offset=20") {
		t.Errorf("redirect = %s", page)
	}
	page = store.RenderRedirect("abc", 0)
	if strings.Contains(page, "offset") {
		t.Errorf("offset should be omitted at zero: %s", page)
	}
}

func TestRenderErrorPageFallback(t *testing.T) {
	store, _ := fixedStore(t)
	// A page without the header marker still gets the error prepended.
	page := store.RenderErrorPage("err", nil, 0, 0)
	if !strings.Contains(page, "err") {
		t.Errorf("error page = %s", page)
	}
}

func TestHandlerReplyWithoutThread(t *testing.T) {
	store, _ := fixedStore(t)
	handler := NewHandler(store)
	form := url.Values{"author": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/board/?new=reply", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid Thread") {
		t.Errorf("reply without thread = %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerUnknownAction(t *testing.T) {
	store, _ := fixedStore(t)
	handler := NewHandler(store)
	form := url.Values{"author": {"x"}, "body": {"y"}}
	req := httptest.NewRequest(http.MethodPost, "/board/?new=bogus", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Invalid Thread") {
		t.Errorf("unknown action = %d %s", rec.Code, rec.Body.String())
	}
}

func TestHandlerGetOnPostAction(t *testing.T) {
	store, _ := fixedStore(t)
	handler := NewHandler(store)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/?new=thread", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET on post action = %d, want 405", rec.Code)
	}
}

func TestHandlerBadForm(t *testing.T) {
	store, _ := fixedStore(t)
	handler := NewHandler(store)
	req := httptest.NewRequest(http.MethodPost, "/board/?new=thread", strings.NewReader("%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("bad form = %d, want 400", rec.Code)
	}
}

func TestHandlerReplyMissingBody(t *testing.T) {
	store, _ := fixedStore(t)
	hash, _ := store.NewThread("a", "s", "b")
	handler := NewHandler(store)
	form := url.Values{"author": {"x"}}
	req := httptest.NewRequest(http.MethodPost, "/board/?new=reply&thread="+hash, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "No body text") {
		t.Errorf("missing body reply = %s", rec.Body.String())
	}
}

func TestHandlerDefaultAuthor(t *testing.T) {
	store, dir := fixedStore(t)
	handler := NewHandler(store)
	form := url.Values{"subject": {"s"}, "body": {"b"}}
	req := httptest.NewRequest(http.MethodPost, "/board/?new=thread", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "index.txt"))
	if !strings.Contains(string(data), "Anonymous Coward") {
		t.Errorf("default author missing: %s", data)
	}
}

func TestStoreThreadMissingFile(t *testing.T) {
	store, dir := fixedStore(t)
	// A valid hash naming a missing file must be reported as invalid.
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Thread("deadbeef", 0); err != ErrInvalidThread {
		t.Errorf("missing thread err = %v", err)
	}
}

func TestStoreNewCreatesDataDirectory(t *testing.T) {
	dir := t.TempDir()
	cfg := Defaults(filepath.Join(dir, "nested", "forumspace"))
	store := New(cfg)
	if _, err := store.NewThread("a", "s", "b"); err != nil {
		t.Fatalf("new thread: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nested", "forumspace", "index.txt")); err != nil {
		t.Errorf("index not created: %v", err)
	}
}

func TestStoreReplyToCorruptIndexLine(t *testing.T) {
	store, dir := fixedStore(t)
	hash, _ := store.NewThread("a", "s", "b")
	// A short line in the index is skipped rather than crashing.
	index, _ := os.ReadFile(filepath.Join(dir, "index.txt"))
	if err := os.WriteFile(filepath.Join(dir, "index.txt"), append(index, []byte("\nshort\tline")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Reply(hash, "x", "y"); err != nil {
		t.Errorf("reply with corrupt index line: %v", err)
	}
}

func TestRenderIndexWithOffsetPaging(t *testing.T) {
	store, _ := fixedStore(t)
	cfg := store.Config()
	cfg.IndexPageSize = 2
	store = New(cfg)
	for i := 0; i < 5; i++ {
		if _, err := store.NewThread("a", "subject", "body"); err != nil {
			t.Fatal(err)
		}
	}
	summaries, total, _ := store.ListThreads(2)
	page := store.RenderIndex(summaries, total, 2)
	if !strings.Contains(page, "offset=0") || !strings.Contains(page, "offset=4") {
		t.Errorf("paging links wrong: %s", page)
	}
}

func TestConcurrentNewThreads(t *testing.T) {
	store, dir := fixedStore(t)
	done := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := store.NewThread("a", "s", "b")
			done <- err
		}()
	}
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent new thread: %v", err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(dir, "index.txt"))
	if lines := strings.Count(string(data), "\n") + 1; lines != 5 {
		t.Errorf("index lines = %d, want 5:\n%s", lines, data)
	}
}

func TestThreadOffsetOutOfRangeDoesNotPanic(t *testing.T) {
	store, _ := fixedStore(t)
	hash, _ := store.NewThread("a", "s", "one")
	// Offsets past the end of the thread must render an empty page, like
	// Python's list slicing, rather than panicking.
	for _, offset := range []int{1, 2, 100, 1 << 20} {
		thread, err := store.Thread(hash, offset)
		if err != nil {
			t.Fatalf("offset %d: %v", offset, err)
		}
		if len(thread.Posts) != 0 {
			t.Errorf("offset %d returned %d posts", offset, len(thread.Posts))
		}
	}
}

func TestThreadWithNoPostsAtOffsetOne(t *testing.T) {
	store, dir := fixedStore(t)
	hash := "abcdef0123456789abcdef0123456789"
	if err := os.WriteFile(filepath.Join(dir, hash), []byte(hash+"\tsubject\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Thread(hash, 1); err != nil {
		t.Fatalf("empty thread with offset 1: %v", err)
	}
}

func TestHandlerExtremeOffset(t *testing.T) {
	store, _ := fixedStore(t)
	hash, _ := store.NewThread("a", "s", "body")
	handler := NewHandler(store)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/board/?thread="+hash+"&offset=99999", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

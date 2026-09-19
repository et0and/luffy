package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseBasics(t *testing.T) {
	input := `
# comment
; another comment
PLAIN=value
QUOTED="hello world"
SINGLE='literal $NOT_A_VAR'
TRAILING=value  # a comment
ESCAPED="a \"quoted\" word"
EMPTY=
export EXPORTED=yes
`
	p := NewParser(".")
	values, err := p.ParseReader("test.conf", strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]string{
		"PLAIN":    "value",
		"QUOTED":   "hello world",
		"SINGLE":   "literal $NOT_A_VAR",
		"TRAILING": "value",
		"ESCAPED":  `a "quoted" word`,
		"EMPTY":    "",
		"EXPORTED": "yes",
	}
	for key, expected := range want {
		if got := values[key]; got != expected {
			t.Errorf("%s = %q, want %q", key, got, expected)
		}
	}
}

func TestParseExpansion(t *testing.T) {
	input := `
BASE=/opt/piratebox
SUB="$BASE/share"
BRACED="${BASE}/www"
CONCAT=$BASE.$BASE
MISSING=$DOES_NOT_EXIST
UNKNOWN=${NOPE}
`
	p := NewParser(".")
	values, err := p.ParseReader("test.conf", strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got, want := values["SUB"], "/opt/piratebox/share"; got != want {
		t.Errorf("SUB = %q, want %q", got, want)
	}
	if got, want := values["BRACED"], "/opt/piratebox/www"; got != want {
		t.Errorf("BRACED = %q, want %q", got, want)
	}
	if got, want := values["CONCAT"], "/opt/piratebox./opt/piratebox"; got != want {
		t.Errorf("CONCAT = %q, want %q", got, want)
	}
	if got := values["MISSING"]; got != "" {
		t.Errorf("MISSING = %q, want empty", got)
	}
	if got := values["UNKNOWN"]; got != "" {
		t.Errorf("UNKNOWN = %q, want empty", got)
	}
}

func TestParseInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "conf", "ipv6.conf"), "IPV6_ENABLE=\"yes\"\nIPV6_PREFIX=\"fdc0:ffea\"\n")
	writeFile(t, filepath.Join(dir, "conf", "hook_custom.conf"), "# empty custom hook config\n")
	main := `
PIRATEBOX_FOLDER="` + dir + `"
. $PIRATEBOX_FOLDER/conf/ipv6.conf
.  $PIRATEBOX_FOLDER/conf/hook_custom.conf
source missing.conf
AFTER=$IPV6_PREFIX
`
	mainPath := filepath.Join(dir, "piratebox.conf")
	writeFile(t, mainPath, main)
	// Includes resolve against the directory of the parsed file.
	p := NewParser(dir)
	values, err := p.ParseFile(mainPath)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := values["IPV6_ENABLE"]; got != "yes" {
		t.Errorf("IPV6_ENABLE = %q, want yes", got)
	}
	if got := values["AFTER"]; got != "fdc0:ffea" {
		t.Errorf("AFTER = %q, want fdc0:ffea", got)
	}
	if len(p.Warnings) == 0 {
		t.Error("expected warning about missing include")
	}
}

func TestParseIncludeNestedRelative(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "conf")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sub, "inner.conf"), "INNER=ok\n")
	writeFile(t, filepath.Join(sub, "outer.conf"), ". inner.conf\nOUTER=$INNER\n")
	main := filepath.Join(dir, "main.conf")
	writeFile(t, main, ". conf/outer.conf\n")

	p := NewParser(dir)
	values, err := p.ParseFile(main)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if values["INNER"] != "ok" || values["OUTER"] != "ok" {
		t.Errorf("nested include failed: %#v", values)
	}
}

func TestParseIncludeCycle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.conf"), ". b.conf\n")
	writeFile(t, filepath.Join(dir, "b.conf"), ". a.conf\n")
	p := NewParser(dir)
	if _, err := p.ParseFile(filepath.Join(dir, "a.conf")); err != nil {
		t.Fatalf("cycle should not be fatal: %v", err)
	}
	found := false
	for _, w := range p.Warnings {
		if strings.Contains(w, "cycle") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cycle warning, got %v", p.Warnings)
	}
}

func TestParseCommandSubstitution(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "version"), "piratebox-ws_1.1.4\n")
	input := "VERSION=$(cat version)\nSHORTCUT=$(<version)\nBAD=$(uname -a)\n"
	p := NewParser(dir)
	values, err := p.ParseReader("test.conf", strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := values["VERSION"]; got != "piratebox-ws_1.1.4" {
		t.Errorf("VERSION = %q", got)
	}
	if got := values["SHORTCUT"]; got != "piratebox-ws_1.1.4" {
		t.Errorf("SHORTCUT = %q", got)
	}
	if got := values["BAD"]; got != "" {
		t.Errorf("BAD = %q, want empty", got)
	}
	if len(p.Warnings) != 1 {
		t.Errorf("expected one warning, got %v", p.Warnings)
	}
}

func TestParseDestinations(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"[ #255.255.255.255# , #10.255.255.255# , #192.168.77.255# ]",
			[]string{"255.255.255.255", "10.255.255.255", "192.168.77.255"}},
		{`[ "ff02::1", "255.255.255.255" ]`, []string{"ff02::1", "255.255.255.255"}},
		{"1.2.3.4", []string{"1.2.3.4"}},
		{"[ #ff02::1# ]", []string{"ff02::1"}},
		{"[,,]", nil},
	}
	for _, c := range cases {
		got := ParseDestinations(c.in)
		if len(got) != len(c.want) {
			t.Errorf("ParseDestinations(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseDestinations(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestLoadRealWorldConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ipv6.conf"), "IPV6_ENABLE=\"no\"\nIPV6_ADVERT=\"radvd\"\nIPV6_IP=\"fdc0:ffea::1\"\nIPV6_MASK=\"64\"\nIPV6_PREFIX=\"fdc0:ffea\"\n")
	writeFile(t, filepath.Join(dir, "hook_custom.conf"), "# custom hooks\n")
	main := `
PIRATEBOX_FOLDER="/opt/piratebox"
WWW_FOLDER="/opt/piratebox/www"
WWW_CONTENT="/opt/piratebox/share/content"
SHARE_FOLDER="$PIRATEBOX_FOLDER/share"
UPLOADFOLDER="$SHARE_FOLDER/Shared"
DO_IFCONFIG="yes"
USE_APN="yes"
USE_DNSMASQ="yes"
HOST="piratebox.lan"
INTERFACE="wlan0"
DNSMASQ_INTERFACE="wlan0"
NET=192.168.77
IP_SHORT=1
START_LEASE=10
END_LEASE=250
LEASE_DURATION=12h
LEASE_FILE_LOCATION=$PIRATEBOX_FOLDER/tmp/lease.file
NETMASK=255.255.255.0
IP=$NET.$IP_SHORT
. $PIRATEBOX_FOLDER/ipv6.conf
CHATFILE="$WWW_FOLDER/cgi-bin/data.pso"
GEN_CHATFILE="$WWW_FOLDER/chat_content.html"
GEN_DISKFILE="$WWW_FOLDER/diskusage.html"
GLOBAL_CHAT="yes"
GLOBAL_DEST="[ #255.255.255.255# , #10.255.255.255# , #$NET.255# ]"
. $PIRATEBOX_FOLDER/hook_custom.conf
`
	path := filepath.Join(dir, "piratebox.conf")
	writeFile(t, path, main)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.IP() != "192.168.77.1" {
		t.Errorf("IP = %q", cfg.IP())
	}
	if cfg.ChatFile != "/opt/piratebox/www/cgi-bin/data.pso" {
		t.Errorf("ChatFile = %q", cfg.ChatFile)
	}
	if !cfg.GlobalChat {
		t.Error("GlobalChat should be enabled")
	}
	if len(cfg.GlobalDest) != 3 || cfg.GlobalDest[2] != "192.168.77.255" {
		t.Errorf("GlobalDest = %v", cfg.GlobalDest)
	}
	if cfg.UploadMaxBytes != 5<<20 {
		t.Errorf("UploadMaxBytes = %d", cfg.UploadMaxBytes)
	}
	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("unexpected validation errors: %v", errs)
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.conf")); err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestFromValuesDefaults(t *testing.T) {
	cfg := FromValues(Values{})
	if cfg.PirateboxFolder != "/opt/piratebox" {
		t.Errorf("default piratebox folder = %q", cfg.PirateboxFolder)
	}
	if cfg.IP() != "192.168.77.1" {
		t.Errorf("default IP = %q", cfg.IP())
	}
	if cfg.HTTPPort != 80 {
		t.Errorf("default HTTP port = %d", cfg.HTTPPort)
	}
	if cfg.UploadHTTPPort() != 8080 {
		t.Errorf("default upload port = %d", cfg.UploadHTTPPort())
	}
	if !cfg.ShoutboxEnabled || !cfg.DroopyEnabled {
		t.Error("defaults should enable shoutbox and uploads")
	}
	if cfg.ShoutboxClientTime {
		t.Error("client timestamps should be off by default")
	}
}

func TestNodeHostname(t *testing.T) {
	cfg := FromValues(Values{"NODE_CONFIG_ACTIVE": "yes", "NODE_NAME": "node1", "NODE_GEN": "yes", "HOST": "piratebox.lan"})
	if name, ok := cfg.NodeHostname(); !ok || name != "node1.piratebox.lan" {
		t.Errorf("NodeHostname = %q, %v", name, ok)
	}
	cfg = FromValues(Values{"NODE_CONFIG_ACTIVE": "yes", "NODE_NAME": "node1", "NODE_GEN": "no"})
	if name, _ := cfg.NodeHostname(); name != "node1" {
		t.Errorf("NodeHostname without suffix = %q", name)
	}
	cfg = FromValues(Values{"NODE_CONFIG_ACTIVE": "yes", "NODE_NAME": ""})
	if name, _ := cfg.NodeHostname(); name != "luffy.lan" {
		t.Errorf("NodeHostname fallback = %q", name)
	}
	cfg = FromValues(Values{"NODE_CONFIG_ACTIVE": "no"})
	if _, ok := cfg.NodeHostname(); ok {
		t.Error("inactive node config should not produce a name")
	}
}

func TestValidateErrors(t *testing.T) {
	cfg := FromValues(Values{
		"INTERFACE":   "",
		"IP_SHORT":    "",
		"NET":         "",
		"NETMASK":     "",
		"HTTP_PORT":   "70000",
		"START_LEASE": "",
		"END_LEASE":   "",
	})
	cfg.HTTPPort = 70000
	errs := cfg.Validate()
	if len(errs) < 4 {
		t.Errorf("expected multiple validation errors, got %v", errs)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIPFallbackFromNetAndShort(t *testing.T) {
	cfg := FromValues(Values{"NET": "10.0.0", "IP_SHORT": "5"})
	delete(cfg.Values, "IP")
	if got := cfg.IP(); got != "10.0.0.5" {
		t.Errorf("IP = %q", got)
	}
	cfg = FromValues(Values{"IP": "1.2.3.4", "NET": "10.0.0", "IP_SHORT": "5"})
	if got := cfg.IP(); got != "1.2.3.4" {
		t.Errorf("explicit IP = %q", got)
	}
}

func TestWarningsExposed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "bad.conf"), "not an assignment\n. missing.conf\nGOOD=yes\n")
	cfg, err := Load(filepath.Join(dir, "bad.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Warnings()) == 0 {
		t.Error("expected warnings to be exposed")
	}
}

func TestGetDefault(t *testing.T) {
	v := Values{"SET": "value", "EMPTY": "", "SPACES": "   "}
	if got := v.GetDefault("SET", "fallback"); got != "value" {
		t.Errorf("SET = %q", got)
	}
	for _, key := range []string{"EMPTY", "SPACES", "MISSING"} {
		if got := v.GetDefault(key, "fallback"); got != "fallback" {
			t.Errorf("%s = %q, want fallback", key, got)
		}
	}
}

func TestInvalidKeysAndLinesAreSkipped(t *testing.T) {
	input := "9INVALID=value\nGOOD=yes\n\n# comment\nBAD LINE HERE\n"
	p := NewParser(".")
	values, err := p.ParseReader("test.conf", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if values["GOOD"] != "yes" {
		t.Errorf("GOOD = %q", values["GOOD"])
	}
	if _, ok := values["9INVALID"]; ok {
		t.Error("invalid key should be skipped")
	}
	if len(p.Warnings) != 2 {
		t.Errorf("warnings = %v", p.Warnings)
	}
}

func TestParseFileMissing(t *testing.T) {
	p := NewParser(t.TempDir())
	if _, err := p.ParseFile("nope.conf"); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParseReaderIncludeDepth(t *testing.T) {
	dir := t.TempDir()
	// A chain deeper than MaxDepth must error rather than recurse forever.
	for i := 0; i < 25; i++ {
		next := fmt.Sprintf("f%d.conf", i+1)
		if i == 24 {
			next = ""
		}
		content := "KEY=value\n"
		if next != "" {
			content += ". " + next + "\n"
		}
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d.conf", i)), content)
	}
	p := NewParser(dir)
	p.MaxDepth = 5
	if _, err := p.ParseFile(filepath.Join(dir, "f0.conf")); err == nil {
		t.Error("expected include depth error")
	}
}

func TestExpansionWithBackslashes(t *testing.T) {
	input := `A="one\\two"
B="say \"hi\""
C='single \ backslash'
D=plain\value
`
	p := NewParser(".")
	values, err := p.ParseReader("t.conf", strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != `one\two` {
		t.Errorf("A = %q", values["A"])
	}
	if values["B"] != `say "hi"` {
		t.Errorf("B = %q", values["B"])
	}
	if values["C"] != `single \ backslash` {
		t.Errorf("C = %q", values["C"])
	}
	if values["D"] != `plain\value` {
		t.Errorf("D = %q", values["D"])
	}
}

func TestUnterminatedQuote(t *testing.T) {
	p := NewParser(".")
	values, err := p.ParseReader("t.conf", strings.NewReader("A=\"unterminated\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "unterminated" {
		t.Errorf("A = %q", values["A"])
	}
}

func TestUnterminatedSingleQuote(t *testing.T) {
	p := NewParser(".")
	values, err := p.ParseReader("t.conf", strings.NewReader("A='unterminated\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "unterminated" {
		t.Errorf("A = %q", values["A"])
	}
}

func TestUnterminatedCommandSubstitution(t *testing.T) {
	p := NewParser(".")
	values, err := p.ParseReader("t.conf", strings.NewReader("A=$(cat file\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] == "" {
		t.Log("unterminated substitution produced empty value (acceptable)")
	}
}

func TestDollarWithoutVariable(t *testing.T) {
	p := NewParser(".")
	values, err := p.ParseReader("t.conf", strings.NewReader("A=price$\nC=${UNCLOSED\nNET=192.168.77\nD=$NET.255\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "price$" {
		t.Errorf("A = %q", values["A"])
	}
	// An unclosed brace is left as literal text rather than swallowing the
	// rest of the value.
	if values["C"] != "${UNCLOSED" {
		t.Errorf("C = %q", values["C"])
	}
	// The common real-world case from GLOBAL_DEST: a variable followed by a
	// dot and digits must not be read as one long variable name.
	if values["D"] != "192.168.77.255" {
		t.Errorf("D = %q", values["D"])
	}
}

func TestIncludeWithoutArgument(t *testing.T) {
	p := NewParser(t.TempDir())
	values, err := p.ParseReader("t.conf", strings.NewReader(".\nsource\nA=1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if values["A"] != "1" {
		t.Errorf("A = %q", values["A"])
	}
	if len(p.Warnings) != 2 {
		t.Errorf("bare include lines should warn and be skipped: %v", p.Warnings)
	}
}

func TestParseDestinationsQuoted(t *testing.T) {
	got := ParseDestinations(`"255.255.255.255"`)
	if len(got) != 1 || got[0] != "255.255.255.255" {
		t.Errorf("quoted = %v", got)
	}
	got = ParseDestinations("[ ]")
	if len(got) != 0 {
		t.Errorf("empty list = %v", got)
	}
}

func TestAtoiDefaultInvalid(t *testing.T) {
	cfg := FromValues(Values{"HTTP_PORT": "not-a-number", "PROBE_TIME": "abc", "UPLOAD_MAX_SIZE_MB": ""})
	if cfg.HTTPPort != 80 {
		t.Errorf("HTTPPort = %d, want default 80", cfg.HTTPPort)
	}
	if cfg.ProbeTime != 10 {
		t.Errorf("ProbeTime = %d, want default 10", cfg.ProbeTime)
	}
	if cfg.UploadMaxBytes != 5<<20 {
		t.Errorf("UploadMaxBytes = %d, want 5MB", cfg.UploadMaxBytes)
	}
}

func TestUploadHTTPPortFallback(t *testing.T) {
	cfg := FromValues(Values{"DROOPY_PORT": ""})
	if got := cfg.UploadHTTPPort(); got != 8080 {
		t.Errorf("UploadHTTPPort = %d", got)
	}
}

func TestValidateUploadSize(t *testing.T) {
	cfg := FromValues(Values{"UPLOAD_MAX_SIZE_MB": "0"})
	found := false
	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "UPLOAD_MAX_SIZE_MB") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected upload size validation error: %v", cfg.Validate())
	}
}

func TestValidateMissingPirateboxFolder(t *testing.T) {
	cfg := FromValues(Values{"PIRATEBOX_FOLDER": ""})
	found := false
	for _, err := range cfg.Validate() {
		if strings.Contains(err.Error(), "PIRATEBOX_FOLDER") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected folder validation error: %v", cfg.Validate())
	}
}

func TestDefaultsAreFullyResolved(t *testing.T) {
	// A minimal config relies entirely on built-in defaults. Every path must
	// come out absolute rather than keeping a literal "$PIRATEBOX_FOLDER".
	cfg := FromValues(Values{})
	for name, value := range map[string]string{
		"WWW_FOLDER":   cfg.WwwFolder,
		"WWW_CONTENT":  cfg.WwwContent,
		"SHARE_FOLDER": cfg.ShareFolder,
		"UPLOADFOLDER": cfg.UploadFolder,
		"TIMESAVE":     cfg.Timesave,
		"NODE_CONFIG":  cfg.NodeConfigPath,
		"CHATFILE":     cfg.ChatFile,
		"GEN_CHATFILE": cfg.GenChatFile,
		"GEN_DISKFILE": cfg.GenDiskFile,
		"LEASE_FILE":   cfg.LeaseFile,
	} {
		if strings.Contains(value, "$") {
			t.Errorf("%s still contains an unexpanded reference: %q", name, value)
		}
		if !strings.HasPrefix(value, "/") {
			t.Errorf("%s is not absolute: %q", name, value)
		}
	}
	if cfg.NodeInterface != cfg.Interface {
		t.Errorf("NODE_INTERFACE = %q, want %q", cfg.NodeInterface, cfg.Interface)
	}
	for _, dest := range cfg.GlobalDest {
		if strings.Contains(dest, "$") {
			t.Errorf("GLOBAL_DEST entry unresolved: %q", dest)
		}
	}
}

func TestDefaultsResolvedInMinimalConfigFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "minimal.conf"), "# only the essentials\nNET=10.0.0\nIP_SHORT=1\n")
	cfg, err := Load(filepath.Join(dir, "minimal.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UploadFolder != "/opt/piratebox/share/Shared" {
		t.Errorf("UploadFolder = %q", cfg.UploadFolder)
	}
	if cfg.HostsFile != "/opt/piratebox/conf/hosts_generated" {
		t.Errorf("HostsFile = %q", cfg.HostsFile)
	}
}

func TestResolveReferencesLeavesUnknownNames(t *testing.T) {
	values := Values{
		"KNOWN":   "value",
		"USES":    "$KNOWN and $UNKNOWN",
		"CYCLE_A": "$CYCLE_B",
		"CYCLE_B": "$CYCLE_A",
	}
	resolved := resolveReferences(values)
	if resolved["USES"] != "value and $UNKNOWN" {
		t.Errorf("USES = %q", resolved["USES"])
	}
	// A cycle must terminate without growing without bound.
	if len(resolved["CYCLE_A"]) > 100 {
		t.Errorf("cycle grew: %q", resolved["CYCLE_A"])
	}
}

func TestResolveReferencesBraced(t *testing.T) {
	values := Values{"BASE": "/opt/pb", "SUB": "${BASE}/www", "MISSING": "${NOPE}/x"}
	resolved := resolveReferences(values)
	if resolved["SUB"] != "/opt/pb/www" {
		t.Errorf("SUB = %q", resolved["SUB"])
	}
	if resolved["MISSING"] != "${NOPE}/x" {
		t.Errorf("MISSING = %q", resolved["MISSING"])
	}
}

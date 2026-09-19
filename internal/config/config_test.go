package config

import (
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
	if name, _ := cfg.NodeHostname(); name != "piratebox.lan" {
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

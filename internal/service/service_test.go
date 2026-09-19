package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"luffy/internal/config"
	"luffy/internal/proc"
)

func testManager(t *testing.T, values config.Values) (*Manager, *proc.Fake, *config.Config, string) {
	t.Helper()
	base := t.TempDir()
	merged := config.Values{
		"PIRATEBOX_FOLDER":   base,
		"WWW_FOLDER":         filepath.Join(base, "www"),
		"WWW_CONTENT":        filepath.Join(base, "share", "content"),
		"SHARE_FOLDER":       filepath.Join(base, "share"),
		"UPLOADFOLDER":       filepath.Join(base, "share", "Shared"),
		"CHATFILE":           filepath.Join(base, "www", "cgi-bin", "data.pso"),
		"GEN_CHATFILE":       filepath.Join(base, "www", "chat_content.html"),
		"GEN_DISKFILE":       filepath.Join(base, "www", "diskusage.html"),
		"TIMESAVE":           filepath.Join(base, "share", "timesave_file"),
		"DO_IFCONFIG":        "yes",
		"USE_APN":            "yes",
		"USE_DNSMASQ":        "yes",
		"FIREWALL_FETCH_DNS": "yes",
		"INTERFACE":          "wlan0",
		"DNSMASQ_INTERFACE":  "wlan0",
		"NET":                "192.168.77",
		"IP_SHORT":           "1",
		"HOST":               "piratebox.lan",
	}
	for k, v := range values {
		merged[k] = v
	}
	cfg := config.FromValues(merged)
	fake := proc.NewFake()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	m := New(cfg, fake, logger)
	return m, fake, cfg, base
}

func TestStartRunsFullSequence(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	lines := fake.Lines()
	for _, want := range []string{
		"ifconfig wlan0 up",
		"ifconfig wlan0 192.168.77.1 netmask 255.255.255.0",
		"hostapd " + filepath.Join(cfg.PirateboxFolder, "conf", "hostapd.conf"),
		"dnsmasq -x " + filepath.Join(cfg.PirateboxFolder, "tmp", "dnsmasq.pid") + " -C " + cfg.DNSMasqConf,
		"iptables -t nat -A PREROUTING -i wlan0 -d 0/0 -p tcp --dport 53 -j DNAT --to-destination 192.168.77.1:53",
		"iptables -t nat -A PREROUTING -i wlan0 -d 0/0 -p udp --dport 53 -j DNAT --to-destination 192.168.77.1:53",
	} {
		if !containsLine(lines, want) {
			t.Errorf("missing command %q in:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	if _, err := os.Stat(cfg.InitDone); err != nil {
		t.Errorf("init_done not written: %v", err)
	}
	for _, file := range []string{cfg.HostsFile, cfg.DNSMasqConf, cfg.ChatFile, cfg.GenChatFile, cfg.GenDiskFile} {
		if _, err := os.Stat(file); err != nil {
			t.Errorf("%s missing: %v", file, err)
		}
	}
}

func containsLine(lines []string, want string) bool {
	for _, line := range lines {
		if strings.Contains(line, want) {
			return true
		}
	}
	return false
}

func TestStartTwiceFails(t *testing.T) {
	m, _, _, _ := testManager(t, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil {
		t.Error("second start should fail")
	}
}

func TestStartSkipsDisabledServices(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"DO_IFCONFIG":         "no",
		"USE_APN":             "no",
		"USE_DNSMASQ":         "no",
		"FIREWALL_FETCH_DNS":  "no",
		"FIREWALL_FETCH_HTTP": "no",
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, line := range fake.Lines() {
		if strings.HasPrefix(line, "ifconfig") || strings.HasPrefix(line, "hostapd") ||
			strings.HasPrefix(line, "dnsmasq") || strings.HasPrefix(line, "iptables") {
			t.Errorf("unexpected command: %s", line)
		}
	}
}

func TestStartFirewallHTTP(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{"FIREWALL_FETCH_HTTP": "yes"})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("iptables", "--dport", "80", "--to-destination", "192.168.77.1:80") {
		t.Errorf("http firewall rule missing: %v", fake.Lines())
	}
}

func TestStopRunsShutdownSequence(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	lines := fake.Lines()
	for _, want := range []string{
		"iptables -t nat -D PREROUTING",
		"ifconfig wlan0 down",
	} {
		if !containsLine(lines, want) {
			t.Errorf("missing stop command %q", want)
		}
	}
	_ = cfg
}

func TestStopUsesPidFile(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	if err := os.MkdirAll(filepath.Join(cfg.PirateboxFolder, "tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(cfg.PirateboxFolder, "tmp", "dnsmasq.pid")
	if err := os.WriteFile(pidPath, []byte("4242\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("kill", "4242") {
		t.Errorf("pid file not used: %v", fake.Lines())
	}
	if _, err := os.Stat(pidPath); err == nil {
		t.Error("pid file should be removed")
	}
}

func TestProbeInterfaceWaits(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"PROBE_INTERFACE": "yes",
		"PROBE_TIME":      "2",
	})
	// ifconfig without arguments is used for probing; make it fail twice.
	attempts := 0
	fake.Errors["ifconfig"] = errors.New("device not found")
	// The fake runner returns the same error for every call, so probing must
	// time out.
	err := m.setupInterface(context.Background())
	if err == nil || !strings.Contains(err.Error(), "did not appear") {
		t.Fatalf("probe error = %v (attempts=%d)", err, attempts)
	}
}

func TestContentFolderCreatedWithEmbeddedAssets(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(cfg.WwwContent, "index.html")
	if _, err := os.Stat(index); err != nil {
		t.Fatalf("embedded index not written: %v", err)
	}
	link := filepath.Join(cfg.WwwFolder, "content")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("content symlink missing: %v", err)
	}
	if target != cfg.WwwContent {
		t.Errorf("symlink target = %q", target)
	}
	shared := filepath.Join(cfg.WwwFolder, "Shared")
	if _, err := os.Readlink(shared); err != nil {
		t.Errorf("Shared symlink missing: %v", err)
	}
}

func TestCustomContentIsPreserved(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	if err := os.MkdirAll(cfg.WwwContent, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := filepath.Join(cfg.WwwContent, "index.html")
	if err := os.WriteFile(custom, []byte("my custom page"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateContentFolder(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(custom)
	if string(data) != "my custom page" {
		t.Errorf("custom content overwritten: %q", data)
	}
}

func TestShoutboxResetAndReadOnly(t *testing.T) {
	m, _, cfg, _ := testManager(t, config.Values{
		"RESET_CHAT":       "yes",
		"SHOUTBOX_ENABLED": "no",
	})
	if err := os.MkdirAll(filepath.Join(cfg.PirateboxFolder, "conf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.PirateboxFolder, "conf", "chat_init.txt"),
		[]byte("<div>init message</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.ChatFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "init message") {
		t.Errorf("chat not reset: %q", data)
	}
	info, err := os.Stat(cfg.ChatFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Errorf("disabled shoutbox should be read only, mode = %v", info.Mode())
	}
}

func TestIPv6Setup(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"IPV6_ENABLE": "yes",
		"IPV6_ADVERT": "radvd",
		"IPV6_IP":     "fdc0:ffea::1",
		"IPV6_MASK":   "64",
		"IPV6_PREFIX": "fdc0:ffea",
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("ifconfig", "wlan0", "add", "fdc0:ffea::1/64") {
		t.Errorf("ipv6 address missing: %v", fake.Lines())
	}
	if !fake.Has("ip", "addr", "change", "fdc0:ffea::1/64", "scope", "link") {
		t.Errorf("ipv6 scope missing: %v", fake.Lines())
	}
	if !fake.Has("radvd") {
		t.Errorf("radvd not started: %v", fake.Lines())
	}
	hosts, err := os.ReadFile(m.Cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hosts), "fdc0:ffea::1") {
		t.Errorf("ipv6 hosts entry missing: %s", hosts)
	}
	radvd, err := os.ReadFile(m.Cfg.RadvdConf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(radvd), "prefix fdc0:ffea::/64") {
		t.Errorf("radvd config wrong: %s", radvd)
	}
}

func TestBridgeSetup(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"DO_BRIDGE": "yes",
		"BRIDGE":    "br-lan",
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("brctl", "addif", "br-lan", "wlan0") {
		t.Errorf("bridge command missing: %v", fake.Lines())
	}
}

func TestNodeAddressSetup(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"NODE_CONFIG_ACTIVE": "yes",
		"NODE_IPV6_SET_IP":   "yes",
		"NODE_IPV6_IP":       "fdc0:ffea::99",
		"NODE_IPV6_MASK":     "/64",
		"NODE_INTERFACE":     "wlan0",
	})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("ifconfig", "wlan0", "add", "fdc0:ffea::99/64") {
		t.Errorf("node address missing: %v", fake.Lines())
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("ifconfig", "wlan0", "del", "fdc0:ffea::99/64") {
		t.Errorf("node address not removed: %v", fake.Lines())
	}
}

func TestHooksRun(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	hookDir := filepath.Join(cfg.PirateboxFolder, "bin", "hooks")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hookDir, "hook_piratebox_start.sh")
	if err := os.WriteFile(hookPath, []byte("#!/bin/sh\necho hook ran\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("/bin/sh", hookPath, cfg.Path) {
		t.Errorf("hook not executed: %v", fake.Lines())
	}
}

func TestFirstRunOnlyOnce(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	if err := m.FirstRunSetup(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(cfg.InitDone)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.FirstRunSetup(context.Background()); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(cfg.InitDone)
	if string(first) != string(second) {
		t.Errorf("init_done rewritten: %q -> %q", first, second)
	}
}

func TestEmptyTmp(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	tmp := filepath.Join(cfg.PirateboxFolder, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "junk.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.emptyTmp(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Errorf("tmp not emptied: %v", entries)
	}
}

func TestTimesaveHelper(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	saver := m.Timesave()
	if saver.Path != cfg.Timesave {
		t.Errorf("timesave path = %q", saver.Path)
	}
}

func TestStationsTrackerInterface(t *testing.T) {
	m, fake, _, _ := testManager(t, nil)
	fake.Outputs["iw"] = "Station aa\nStation bb\nStation cc\n"
	tracker := m.StationsTracker()
	if err := tracker.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if tracker.Count() != 3 {
		t.Errorf("count = %d", tracker.Count())
	}
}

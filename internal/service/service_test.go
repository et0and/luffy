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

func TestNewDefaultsRunnerAndLogger(t *testing.T) {
	cfg := config.FromValues(config.Values{"PIRATEBOX_FOLDER": t.TempDir()})
	m := New(cfg, nil, nil)
	if m.Runner == nil || m.Logger == nil || m.Now == nil {
		t.Errorf("New did not fill defaults: %+v", m)
	}
}

func TestStartWithIWInterface(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{"DO_IW": "yes", "PHY_IF": "phy0"})
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("iw", "phy0", "interface", "add", "wlan0", "type", "managed") {
		t.Errorf("iw command missing: %v", fake.Lines())
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.Has("iw", "dev", "wlan0", "del") {
		t.Errorf("iw delete missing: %v", fake.Lines())
	}
}

func TestProbeInterfaceSucceedsWhenPresent(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"PROBE_INTERFACE": "yes",
		"PROBE_TIME":      "3",
	})
	// The fake runner succeeds by default, so probing returns immediately.
	if err := m.probeInterface(context.Background()); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if len(fake.Commands) != 1 {
		t.Errorf("expected one probe attempt: %v", fake.Lines())
	}
}

func TestProbeInterfaceContextCancelled(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{
		"PROBE_INTERFACE": "yes",
		"PROBE_TIME":      "10",
	})
	fake.Errors["ifconfig"] = errors.New("missing")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.probeInterface(ctx); err == nil {
		t.Error("expected cancellation error")
	}
}

func TestStopInterfaceSkipsWhenDisabled(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{"DO_IFCONFIG": "no", "DO_IW": "no"})
	if err := m.stopInterface(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("no commands expected: %v", fake.Lines())
	}
}

func TestStopProcessFallsBackToPkill(t *testing.T) {
	m, fake, _, _ := testManager(t, nil)
	m.stopProcess(context.Background(), "hostapd", "hostapd")
	if !fake.Has("pkill", "-f", "hostapd") {
		t.Errorf("pkill fallback missing: %v", fake.Lines())
	}
}

func TestStopProcessIgnoresBadPidFile(t *testing.T) {
	m, fake, cfg, _ := testManager(t, nil)
	tmp := filepath.Join(cfg.PirateboxFolder, "tmp")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "dnsmasq.pid"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.stopProcess(context.Background(), "dnsmasq", "dnsmasq")
	if !fake.Has("pkill") {
		t.Errorf("bad pid file should fall back to pkill: %v", fake.Lines())
	}
}

func TestEmptyTmpMissingDirectory(t *testing.T) {
	m, _, _, _ := testManager(t, nil)
	if err := m.emptyTmp(); err != nil {
		t.Errorf("missing tmp folder should not fail: %v", err)
	}
}

func TestFirstRunSetupWritesInitDoneFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	m, _, cfg, _ := testManager(t, nil)
	// A file where the conf directory belongs makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(cfg.PirateboxFolder, "conf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.FirstRunSetup(context.Background()); err == nil {
		t.Error("expected setup error")
	}
}

func TestPrepareShoutboxWithoutInitFile(t *testing.T) {
	m, _, cfg, _ := testManager(t, config.Values{"RESET_CHAT": "yes"})
	// conf/chat_init.txt does not exist: the chat is reset to empty.
	if err := m.prepareShoutbox(); err != nil {
		t.Fatalf("prepare shoutbox: %v", err)
	}
	data, err := os.ReadFile(cfg.ChatFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Errorf("chat should be empty, got %q", data)
	}
}

func TestPrepareShoutboxKeepsHistory(t *testing.T) {
	m, _, cfg, _ := testManager(t, config.Values{"RESET_CHAT": "no"})
	if err := os.MkdirAll(filepath.Dir(cfg.ChatFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ChatFile, []byte("<div>existing</div>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.prepareShoutbox(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.ChatFile)
	if !strings.Contains(string(data), "existing") {
		t.Errorf("history lost: %q", data)
	}
}

func TestCreateContentFolderExistingSkipsCopy(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	if err := os.MkdirAll(cfg.WwwContent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.CreateContentFolder(); err != nil {
		t.Fatal(err)
	}
	// An existing content folder is left untouched, so the shipped index is
	// not copied in.
	if _, err := os.Stat(filepath.Join(cfg.WwwContent, "index.html")); err == nil {
		t.Error("existing content folder should not be repopulated")
	}
}

func TestCopyAssetsSkipsExistingFiles(t *testing.T) {
	target := t.TempDir()
	if err := os.WriteFile(filepath.Join(target, "index.html"), []byte("custom"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := CopyAssets(target); err != nil {
		t.Fatalf("copy assets: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(target, "index.html"))
	if string(data) != "custom" {
		t.Errorf("existing file overwritten: %q", data)
	}
	// Other files are still copied in.
	if _, err := os.Stat(filepath.Join(target, "css", "page_style.css")); err != nil {
		t.Errorf("css not copied: %v", err)
	}
}

func TestCopyAssetsCreatesSubdirectories(t *testing.T) {
	target := filepath.Join(t.TempDir(), "deep", "nested")
	if err := CopyAssets(target); err != nil {
		t.Fatalf("copy assets: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "js", "scripts.js")); err != nil {
		t.Errorf("nested asset missing: %v", err)
	}
}

func TestDistributeFilesMissingSource(t *testing.T) {
	root := t.TempDir()
	if err := DistributeFiles(root, filepath.Join(t.TempDir(), "empty"), false); err != nil {
		t.Errorf("missing source files should not fail: %v", err)
	}
}

func TestRemovePartialUploadsMissingDirectory(t *testing.T) {
	removed, err := RemovePartialUploads(filepath.Join(t.TempDir(), "missing"))
	if err != nil || removed != 0 {
		t.Errorf("RemovePartialUploads = %d, %v", removed, err)
	}
}

func TestDeleteEmptyFilesMissingRoot(t *testing.T) {
	deleted, err := DeleteEmptyFiles(filepath.Join(t.TempDir(), "missing"))
	if err != nil || deleted != 0 {
		t.Errorf("DeleteEmptyFiles = %d, %v", deleted, err)
	}
}

func TestSetHostnameWithoutConfigPath(t *testing.T) {
	_, _, cfg, _ := testManager(t, nil)
	cfg.Path = ""
	if err := SetHostname(cfg, "box.lan"); err != nil {
		t.Fatalf("set hostname without config: %v", err)
	}
	if cfg.Host != "box.lan" {
		t.Errorf("host = %q", cfg.Host)
	}
}

func TestSetHostnameUnwritableConfig(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permissions")
	}
	_, _, cfg, base := testManager(t, nil)
	confPath := filepath.Join(base, "readonly.conf")
	if err := os.WriteFile(confPath, []byte("HOST=\"old\"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	cfg.Path = confPath
	if err := SetHostname(cfg, "new.lan"); err == nil {
		t.Error("expected error writing read-only config")
	}
}

func TestStartCleansUploadFolder(t *testing.T) {
	m, _, cfg, _ := testManager(t, nil)
	if err := os.MkdirAll(cfg.UploadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "empty"), ""); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "tmpdroopy1"), "partial"); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "keep.txt"), "real"); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.UploadFolder, "empty")); err == nil {
		t.Error("zero byte upload should be removed on start")
	}
	if _, err := os.Stat(filepath.Join(cfg.UploadFolder, "tmpdroopy1")); err == nil {
		t.Error("partial upload should be removed on start")
	}
	if _, err := os.Stat(filepath.Join(cfg.UploadFolder, "keep.txt")); err != nil {
		t.Errorf("real file should be kept: %v", err)
	}
}

func TestStartSkipsUploadCleanupWhenDisabled(t *testing.T) {
	m, _, cfg, _ := testManager(t, config.Values{"DROOPY_ENABLED": "no"})
	if err := os.MkdirAll(cfg.UploadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(filepath.Join(cfg.UploadFolder, "empty"), ""); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.UploadFolder, "empty")); err != nil {
		t.Error("cleanup should not run when uploads are disabled")
	}
}

func TestHooksHaveTimeout(t *testing.T) {
	m, _, _, _ := testManager(t, nil)
	if m.Hooks().Timeout <= 0 {
		t.Error("production hook runner must set a timeout")
	}
}

func TestSetHostnameRejectsNewlines(t *testing.T) {
	_, _, cfg, _ := testManager(t, nil)
	for _, bad := range []string{"a\nb", "a\rb", "a\nHOST=\"evil\""} {
		if err := SetHostname(cfg, bad); err == nil {
			t.Errorf("SetHostname(%q) should be rejected", bad)
		}
	}
}

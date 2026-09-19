package sysconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"luffy/internal/config"
)

func testConfig(t *testing.T, extra config.Values) (*config.Config, string) {
	t.Helper()
	base := t.TempDir()
	values := config.Values{
		"PIRATEBOX_FOLDER":  base,
		"WWW_FOLDER":        filepath.Join(base, "www"),
		"WWW_CONTENT":       filepath.Join(base, "share", "content"),
		"SHARE_FOLDER":      filepath.Join(base, "share"),
		"UPLOADFOLDER":      filepath.Join(base, "share", "Shared"),
		"CHATFILE":          filepath.Join(base, "www", "cgi-bin", "data.pso"),
		"GEN_CHATFILE":      filepath.Join(base, "www", "chat_content.html"),
		"GEN_DISKFILE":      filepath.Join(base, "www", "diskusage.html"),
		"NET":               "192.168.77",
		"IP_SHORT":          "1",
		"HOST":              "piratebox.lan",
		"DNSMASQ_INTERFACE": "wlan0",
		"START_LEASE":       "10",
		"END_LEASE":         "250",
		"LEASE_DURATION":    "12h",
	}
	for k, v := range extra {
		values[k] = v
	}
	cfg := config.FromValues(values)
	if err := os.MkdirAll(filepath.Join(base, "conf", "avahi"), 0o755); err != nil {
		t.Fatal(err)
	}
	return cfg, base
}

func TestGenerateHosts(t *testing.T) {
	cfg, base := testConfig(t, nil)
	if err := os.WriteFile(filepath.Join(base, "conf", "hosts"),
		[]byte("#------ Static\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := New(cfg)
	if err := gen.GenerateHosts(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "#------ Static") {
		t.Errorf("static hosts content lost: %s", content)
	}
	if !strings.Contains(content, "192.168.77.1     piratebox.lan") {
		t.Errorf("ipv4 entry missing: %s", content)
	}
}

func TestGenerateHostsIPv6(t *testing.T) {
	cfg, _ := testConfig(t, config.Values{
		"IPV6_ENABLE": "yes",
		"IPV6_IP":     "fdc0:ffea::1",
	})
	gen := New(cfg)
	if err := gen.GenerateHosts(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.HostsFile)
	if !strings.Contains(string(data), "fdc0:ffea::1     piratebox.lan") {
		t.Errorf("ipv6 entry missing: %s", data)
	}
}

func TestGenerateHostsNode(t *testing.T) {
	cfg, _ := testConfig(t, config.Values{
		"NODE_CONFIG_ACTIVE": "yes",
		"NODE_NAME":          "node7",
		"NODE_GEN":           "yes",
		"NODE_IPV6_IP":       "fdc0:ffea::7",
	})
	gen := New(cfg)
	if err := gen.GenerateHosts(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.HostsFile)
	if !strings.Contains(string(data), "fdc0:ffea::7   node7.piratebox.lan") {
		t.Errorf("node entry missing: %s", data)
	}
}

func TestGenerateDNSMasq(t *testing.T) {
	cfg, base := testConfig(t, nil)
	if err := os.WriteFile(filepath.Join(base, "conf", "dnsmasq_default.conf"),
		[]byte("no-resolv\nno-poll\nno-hosts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := New(cfg)
	if err := gen.GenerateDNSMasq(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.DNSMasqConf)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"no-resolv",
		"interface=wlan0",
		"dhcp-range=192.168.77.10,192.168.77.250,12h",
		"address=/#/192.168.77.1",
		"dhcp-leasefile=" + cfg.LeaseFile,
		"addn-hosts=" + cfg.HostsFile,
		"addn-hosts=" + cfg.HostsMeshFile,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("dnsmasq config missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "dchp-range") {
		t.Errorf("ipv6 range should not be present: %s", content)
	}
}

func TestGenerateDNSMasqIPv6(t *testing.T) {
	cfg, _ := testConfig(t, config.Values{
		"IPV6_ENABLE": "yes",
		"IPV6_ADVERT": "dnsmasq",
		"IPV6_PREFIX": "fdc0:ffea",
	})
	gen := New(cfg)
	if err := gen.GenerateDNSMasq(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfg.DNSMasqConf)
	if !strings.Contains(string(data), "dchp-range=fdc0:ffea::, ra-stateless") {
		t.Errorf("ipv6 range missing: %s", data)
	}
}

func TestGenerateRadvd(t *testing.T) {
	cfg, _ := testConfig(t, config.Values{
		"IPV6_ENABLE": "yes",
		"IPV6_ADVERT": "radvd",
		"IPV6_PREFIX": "fdc0:ffea",
		"IPV6_MASK":   "64",
	})
	gen := New(cfg)
	if err := gen.Generate(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.RadvdConf)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{"interface wlan0", "AdvSendAdvert on;", "prefix fdc0:ffea::/64"} {
		if !strings.Contains(content, want) {
			t.Errorf("radvd config missing %q: %s", want, content)
		}
	}
}

func TestGenerateLighttpdEnv(t *testing.T) {
	cfg, base := testConfig(t, config.Values{
		"GLOBAL_CHAT": "yes",
		"GLOBAL_DEST": "[ #255.255.255.255# , #192.168.77.255# ]",
	})
	gen := New(cfg)
	if err := gen.GenerateLighttpdEnv(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(base, "conf", "lighttpd", "env"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		"SHOUTBOX_GEN_HTMLFILE",
		cfg.GenChatFile,
		"SHOUTBOX_CHATFILE",
		"UPLOAD_PATH",
		"DISK_GEN_HTMLFILE",
		"SHOUTBOX_BROADCAST_DESTINATIONS",
		"#255.255.255.255#",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("env missing %q:\n%s", want, content)
		}
	}
}

func TestGenerateLighttpdEnvWithoutGlobalChat(t *testing.T) {
	cfg, base := testConfig(t, nil)
	gen := New(cfg)
	if err := gen.GenerateLighttpdEnv(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(base, "conf", "lighttpd", "env"))
	if strings.Contains(string(data), "SHOUTBOX_BROADCAST_DESTINATIONS") {
		t.Errorf("broadcast line should be absent: %s", data)
	}
}

func TestGenerateAvahi(t *testing.T) {
	cfg, _ := testConfig(t, nil)
	if err := os.WriteFile(cfg.AvahiSchema,
		[]byte("host-name=#####MASKED_HOSTNAME#####\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gen := New(cfg)
	if err := gen.GenerateAvahi(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(cfg.AvahiConf)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "host-name=piratebox_lan\n" {
		t.Errorf("avahi config = %q", data)
	}
}

func TestGenerateAvahiMissingSchema(t *testing.T) {
	cfg, _ := testConfig(t, nil)
	gen := New(cfg)
	if err := gen.GenerateAvahi(); err != nil {
		t.Fatalf("missing schema should not fail: %v", err)
	}
}

func TestGenerateAll(t *testing.T) {
	cfg, _ := testConfig(t, nil)
	gen := New(cfg)
	if err := gen.Generate(); err != nil {
		t.Fatal(err)
	}
	for _, path := range gen.GeneratedPaths() {
		if path == cfg.AvahiConf || path == cfg.RadvdConf {
			continue // optional outputs: no schema and IPv6 disabled
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s missing: %v", path, err)
		}
	}
}

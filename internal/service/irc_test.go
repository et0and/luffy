package service

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"luffy/internal/config"
)

func TestStartIRCWithBinary(t *testing.T) {
	m, fake, cfg, base := testManager(t, config.Values{"ENABLE_IRC_SERVER": "yes"})
	lookPath = func(name string) (string, error) { return "/usr/bin/miniircd", nil }
	defer func() { lookPath = defaultLookPath }()

	if err := m.startIRC(context.Background()); err != nil {
		t.Fatalf("start irc: %v", err)
	}
	if !fake.Has("/usr/bin/miniircd", "--daemon", "--pid-file") {
		t.Errorf("irc command missing: %v", fake.Lines())
	}
	_ = cfg
	_ = base
}

func TestStartIRCPythonFallback(t *testing.T) {
	m, fake, cfg, _ := testManager(t, config.Values{"ENABLE_IRC_SERVER": "yes"})
	lookPath = func(name string) (string, error) { return "", exec.ErrNotFound }
	defer func() { lookPath = defaultLookPath }()

	script := cfg.PirateboxFolder + "/bin/miniircd.py"
	if err := writeTestFile(script, "#!/usr/bin/python\n"); err != nil {
		t.Fatal(err)
	}
	if err := m.startIRC(context.Background()); err != nil {
		t.Fatalf("start irc: %v", err)
	}
	if !fake.Has("python", script) {
		t.Errorf("python fallback missing: %v", fake.Lines())
	}
}

func TestStartIRCWithoutServer(t *testing.T) {
	m, fake, _, _ := testManager(t, config.Values{"ENABLE_IRC_SERVER": "yes"})
	lookPath = func(name string) (string, error) { return "", exec.ErrNotFound }
	defer func() { lookPath = defaultLookPath }()

	if err := m.startIRC(context.Background()); err != nil {
		t.Fatalf("missing server should not fail start: %v", err)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("no command expected: %v", fake.Lines())
	}
}

func TestStartIRCDisabled(t *testing.T) {
	m, fake, _, _ := testManager(t, nil)
	if err := m.startIRC(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("no command expected: %v", fake.Lines())
	}
}

func TestIRCConfigParsing(t *testing.T) {
	m, _, cfg, base := testManager(t, nil)
	path := base + "/conf/irc.conf"
	if err := writeTestFile(path, `### config
IRC_CONFIG_FOLDER=$PIRATEBOX_FOLDER/conf/irc
#IRC_STATEDIR=$IRC_CONFIG_FOLDER/state
IRC_MOTD=$IRC_CONFIG_FOLDER/motd.txt
`); err != nil {
		t.Fatal(err)
	}
	values := m.ircConfig(path)
	if values["IRC_MOTD"] != cfg.PirateboxFolder+"/conf/irc/motd.txt" {
		t.Errorf("IRC_MOTD = %q", values["IRC_MOTD"])
	}
	if _, ok := values["IRC_STATEDIR"]; ok {
		t.Error("commented state dir should be ignored")
	}
}

func writeTestFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

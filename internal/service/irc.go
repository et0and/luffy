package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"luffy/internal/proc"
)

// startIRC launches the optional IRC server.
//
// The original init script started the bundled miniircd.py. Luffy prefers a
// native `miniircd` binary on PATH and falls back to the Python script so
// existing installations keep working during the migration.
func (m *Manager) startIRC(ctx context.Context) error {
	if !m.Cfg.EnableIRCServer {
		return nil
	}
	conf := filepath.Join(m.Cfg.PirateboxFolder, "conf", "irc.conf")
	ircValues := m.ircConfig(conf)
	motd := ircValues["IRC_MOTD"]
	statedir := ircValues["IRC_STATEDIR"]
	user := ircValues["IRC_USER"]
	if user == "" {
		user = m.Cfg.User
	}

	args := []string{"--daemon", "--pid-file", m.pidFile("irc")}
	if motd != "" {
		args = append(args, "--motd", motd)
	}
	if statedir != "" {
		args = append(args, "--statedir", statedir)
	}
	if user != "" {
		args = append(args, "--setuid", user)
	}

	if path, err := lookPath("miniircd"); err == nil {
		pid, err := m.Runner.Start(ctx, proc.Command{Name: path, Args: args})
		if err != nil {
			return fmt.Errorf("service: start miniircd: %w", err)
		}
		m.Logger.Info("started miniircd", "pid", pid, "binary", path)
		return nil
	}

	script := filepath.Join(m.Cfg.PirateboxFolder, "bin", "miniircd.py")
	if _, err := os.Stat(script); err != nil {
		m.Logger.Warn("IRC server enabled but no miniircd binary or miniircd.py found",
			"hint", "install miniircd or place miniircd.py in "+filepath.Dir(script))
		return nil
	}
	pid, err := m.Runner.Start(ctx, proc.Command{Name: "python", Args: append([]string{script}, args...)})
	if err != nil {
		return fmt.Errorf("service: start miniircd.py: %w", err)
	}
	m.Logger.Info("started miniircd.py", "pid", pid)
	return nil
}

// ircConfig loads the small irc.conf file. Assignments are expanded against
// earlier keys and PIRATEBOX_FOLDER, like the shell sourcing did.
func (m *Manager) ircConfig(path string) map[string]string {
	values := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return values
	}
	expand := func(name string) string {
		if name == "PIRATEBOX_FOLDER" {
			return m.Cfg.PirateboxFolder
		}
		return values[name]
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		values[key] = os.Expand(value, expand)
	}
	return values
}

// lookPath is replaced in tests.
var lookPath = defaultLookPath

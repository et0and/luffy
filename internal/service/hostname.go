package service

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"luffy/internal/config"
)

// redirectTemplate matches src/redirect.html.schema from the original
// project.
const redirectTemplate = `<html>
<head><title>Redirect...</title>
<meta http-equiv='refresh' content='0;url=http://%s/content' />
<meta http-equiv='cache-control' content='no-cache, no-store, must-revalidate' />
<meta http-equiv='pragma' content='no-cache' />
<meta http-equiv='expires' content='0' />
</head>
<body>
Redirect
</body>
</html>
`

// SetHostname ports `install_piratebox.sh <conf> hostname <name>`: the
// visible hostname is written to the redirect page and to the configuration
// file.
func SetHostname(cfg *config.Config, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("service: hostname must not be empty")
	}
	if strings.ContainsAny(name, " \t\"'<>/\\") {
		return fmt.Errorf("service: hostname %q contains invalid characters", name)
	}
	if err := os.MkdirAll(cfg.WwwFolder, 0o755); err != nil {
		return fmt.Errorf("service: create www folder: %w", err)
	}
	page := fmt.Sprintf(redirectTemplate, name)
	if err := os.WriteFile(filepath.Join(cfg.WwwFolder, "index.html"), []byte(page), 0o644); err != nil {
		return fmt.Errorf("service: write index.html: %w", err)
	}
	if cfg.Path == "" {
		return nil
	}
	updated, err := replaceHostLine(cfg.Path, name)
	if err != nil {
		return err
	}
	if updated {
		cfg.Values["HOST"] = name
		cfg.Host = name
	}
	return nil
}

// replaceHostLine rewrites the first HOST= assignment in a configuration
// file, keeping every other line untouched.
func replaceHostLine(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("service: read %s: %w", path, err)
	}
	lines := strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "HOST=") || strings.HasPrefix(trimmed, "export HOST=") {
			prefix := ""
			if strings.HasPrefix(trimmed, "export ") {
				prefix = "export "
			}
			lines[i] = prefix + `HOST="` + name + `"`
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, `HOST="`+name+`"`)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return false, fmt.Errorf("service: write %s: %w", path, err)
	}
	return true, nil
}

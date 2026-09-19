package upload

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const anonDir = "anonymous"

// resolveUserDir implements droopy's subdirectory feature. It returns the
// directory name or false when the request is not allowed.
func (h *Handler) resolveUserDir(r *http.Request) (string, bool) {
	subdir := firstValue(r.MultipartForm.Value["subdir"])
	secret := firstValue(r.MultipartForm.Value["secret"])
	if subdir == "" {
		return anonDir, true
	}
	lowered := strings.ToLower(subdir)
	name, exact := safeDirName(lowered)
	if name == "" || !exact {
		// Droopy refused names that lost characters during sanitising.
		return "", false
	}
	if name == anonDir {
		return anonDir, true
	}
	passwords, err := h.loadPasswords()
	if err != nil {
		h.log().Warn("password file unreadable", "error", err)
		passwords = map[string]string{}
	}
	if existing, ok := passwords[name]; ok {
		if existing != secret {
			return "", false
		}
		return name, true
	}
	passwords[name] = secret
	if err := h.savePasswords(passwords); err != nil {
		h.log().Warn("password file not writable", "error", err)
	}
	return name, true
}

// safeDirName mirrors droopy's _safername: only letters, digits, '-', '_',
// '(', ')', '%' and space survive. The second return value reports whether
// the name was already clean.
func safeDirName(name string) (string, bool) {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '(' || r == ')' || r == '%' || r == ' ':
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	return cleaned, cleaned == name
}

func firstValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func (h *Handler) loadPasswords() (map[string]string, error) {
	passwords := map[string]string{}
	if h.PasswordFile == "" {
		return passwords, nil
	}
	data, err := os.ReadFile(h.PasswordFile)
	if err != nil {
		if os.IsNotExist(err) {
			return passwords, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &passwords); err != nil {
		return nil, fmt.Errorf("upload: parse password file: %w", err)
	}
	return passwords, nil
}

func (h *Handler) savePasswords(passwords map[string]string) error {
	if h.PasswordFile == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.PasswordFile), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(passwords)
	if err != nil {
		return err
	}
	return os.WriteFile(h.PasswordFile, data, 0o600)
}

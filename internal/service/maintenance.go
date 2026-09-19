package service

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"luffy/internal/proc"
)

// DistributeFiles ports bin/distribute_files.sh: copy HEADER.txt and
// README.txt into every directory of the share folder so every listing
// keeps the PirateBox look. Existing files are only replaced when
// overwrite is set.
func DistributeFiles(root, sourceDir string, overwrite bool) error {
	files := []string{"HEADER.txt", "README.txt"}
	for _, name := range files {
		src := filepath.Join(sourceDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() {
				return nil
			}
			target := filepath.Join(path, name)
			if !overwrite {
				if _, err := os.Stat(target); err == nil {
					return nil
				}
			}
			return copyFile(src, target)
		})
		if err != nil {
			return fmt.Errorf("service: distribute %s: %w", name, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// DeleteEmptyFiles ports bin/delete_empty.sh. It removes zero byte files
// that browsers leave behind when an upload is aborted. The function never
// touches the root of the filesystem: an empty or missing root is refused.
func DeleteEmptyFiles(root string) (int, error) {
	if strings.TrimSpace(root) == "" || root == "/" {
		return 0, fmt.Errorf("service: refusing to clean %q", root)
	}
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	deleted := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries, like the shell version did
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() != 0 {
			return nil
		}
		if err := os.Remove(path); err == nil {
			deleted++
		}
		return nil
	})
	return deleted, err
}

// RemovePartialUploads removes leftover tmp* files from an aborted upload,
// matching the `find $UPLOADFOLDER -iname tmp\* -exec rm` call.
func RemovePartialUploads(root string) (int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(strings.ToLower(entry.Name()), "tmp") {
			if err := os.Remove(filepath.Join(root, entry.Name())); err == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// FlushDNSMasq ports bin/flush_dnsmasq.sh: SIGHUP tells dnsmasq to reread
// its hosts files, which the mesh feature relies on.
func (m *Manager) FlushDNSMasq(ctx context.Context) error {
	pidPath := m.pidFile("dnsmasq")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("service: read dnsmasq pid file: %w", err)
	}
	pid := strings.TrimSpace(string(data))
	if pid == "" {
		return fmt.Errorf("service: empty dnsmasq pid file %s", pidPath)
	}
	if _, err := m.Runner.Run(ctx, proc.Command{Name: "kill", Args: []string{"-1", pid}}); err != nil {
		return fmt.Errorf("service: flush dnsmasq: %w", err)
	}
	m.Logger.Info("signalled dnsmasq to reload hosts", "pid", pid)
	return nil
}

package service

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"luffy/assets"
)

// CopyAssets writes the embedded default web content into target. Existing
// files are left untouched so user customisations survive upgrades.
func CopyAssets(target string) error {
	root := assets.WWWContent()
	return fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dest := filepath.Join(target, filepath.FromSlash(path))
		if d.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if _, err := os.Stat(dest); err == nil {
			return nil
		}
		src, err := root.Open(path)
		if err != nil {
			return fmt.Errorf("service: open embedded %s: %w", path, err)
		}
		defer src.Close()
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("service: create %s: %w", dest, err)
		}
		if _, err := io.Copy(out, src); err != nil {
			out.Close()
			return fmt.Errorf("service: copy %s: %w", dest, err)
		}
		return out.Close()
	})
}

package assets

import (
	"io/fs"
	"strings"
	"testing"
)

func TestWWWContentContainsStartPage(t *testing.T) {
	root := WWWContent()
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		t.Fatalf("index.html: %v", err)
	}
	if !strings.Contains(string(index), "Luffy") {
		t.Error("index.html does not mention Luffy")
	}
}

func TestWWWContentIncludesAssets(t *testing.T) {
	root := WWWContent()
	for _, path := range []string{
		"css/page_style.css",
		"css/jquery-ui.min.css",
		"js/scripts.js",
		"js/jquery.min.js",
		"js/l10n.js",
		"img/logo-small.png",
		"img/menu.png",
		"favicon.ico",
		"locales/locales.ini",
		"locales/data.en.properties",
	} {
		if _, err := fs.Stat(root, path); err != nil {
			t.Errorf("%s missing from embedded assets: %v", path, err)
		}
	}
}

func TestWWWContentIsRootedAtContent(t *testing.T) {
	root := WWWContent()
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "www_content" {
			t.Error("root should not contain the www_content prefix directory")
		}
	}
	if len(entries) == 0 {
		t.Error("embedded content is empty")
	}
}

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetHostname(t *testing.T) {
	_, _, cfg, base := testManager(t, nil)
	confPath := filepath.Join(base, "piratebox.conf")
	if err := writeTestFile(confPath, "PIRATEBOX_FOLDER=\""+base+"\"\nHOST=\"piratebox.lan\"\nUSE_APN=\"yes\"\n"); err != nil {
		t.Fatal(err)
	}
	cfg.Path = confPath

	if err := SetHostname(cfg, "mybox.lan"); err != nil {
		t.Fatalf("set hostname: %v", err)
	}
	page, err := os.ReadFile(filepath.Join(cfg.WwwFolder, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "url=http://mybox.lan/content") {
		t.Errorf("redirect page = %s", page)
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `HOST="mybox.lan"`) {
		t.Errorf("config not updated: %s", data)
	}
	if strings.Contains(string(data), `HOST="piratebox.lan"`) {
		t.Errorf("old hostname still present: %s", data)
	}
	if !strings.Contains(string(data), `USE_APN="yes"`) {
		t.Errorf("other settings lost: %s", data)
	}
}

func TestSetHostnameAppendsWhenMissing(t *testing.T) {
	_, _, cfg, base := testManager(t, nil)
	confPath := filepath.Join(base, "piratebox.conf")
	if err := writeTestFile(confPath, "PIRATEBOX_FOLDER=\""+base+"\"\n"); err != nil {
		t.Fatal(err)
	}
	cfg.Path = confPath
	if err := SetHostname(cfg, "newbox"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(confPath)
	if !strings.Contains(string(data), `HOST="newbox"`) {
		t.Errorf("hostname not appended: %s", data)
	}
}

func TestSetHostnameValidation(t *testing.T) {
	_, _, cfg, _ := testManager(t, nil)
	for _, bad := range []string{"", "  ", "bad name", "bad/name", "bad\"name"} {
		if err := SetHostname(cfg, bad); err == nil {
			t.Errorf("SetHostname(%q) should fail", bad)
		}
	}
}

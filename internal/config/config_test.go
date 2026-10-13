package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSharedConfigAndLegacyListenOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("MYSQL_EXPLORER_TEST_PASSWORD", "environment-value")
	if err := os.WriteFile(path, []byte("listen='not-a-listen-address'\n[[clusters]]\nname='test'\naddress='127.0.0.1:3306'\nuser='reader'\npassword='file-value'\npassword_env='MYSQL_EXPLORER_TEST_PASSWORD'\n"), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Read(path)
	if err != nil || c.Clusters[0].Password != "environment-value" {
		t.Fatal("shared config or password precedence failed")
	}
	if _, err := Load([]string{"--config", path}); err == nil {
		t.Fatal("invalid web listen accepted")
	}
	c, err = Load([]string{"--config", path, "--listen", "127.0.0.1:18084"})
	if err != nil || c.Listen != "127.0.0.1:18084" {
		t.Fatal("legacy override failed", err)
	}
}

func TestDefaultPath(t *testing.T) {
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "settings"))
	if got := DefaultPath(); got != filepath.Join(dir, "settings", "mysql-explorer", "config.toml") {
		t.Fatal(got)
	}
	if err := os.WriteFile("config.toml", nil, 0600); err != nil {
		t.Fatal(err)
	}
	if got := DefaultPath(); got != "config.toml" {
		t.Fatal(got)
	}
}

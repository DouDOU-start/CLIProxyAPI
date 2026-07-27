package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestPostgresStoreCloseRemovesOwnedTemporaryWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if errMkdir := os.MkdirAll(workspace, 0o700); errMkdir != nil {
		t.Fatalf("create workspace: %v", errMkdir)
	}
	store := &PostgresStore{spoolRoot: workspace, temporary: true}
	if errClose := store.Close(); errClose != nil {
		t.Fatalf("Close() error = %v", errClose)
	}
	if _, errStat := os.Stat(workspace); !os.IsNotExist(errStat) {
		t.Fatalf("temporary workspace still exists after Close(): %v", errStat)
	}
}

func TestNormalizePersistentConfigRemovesLocalAndDeprecatedFields(t *testing.T) {
	normalized, errNormalize := normalizePersistentConfig([]byte(`host: 127.0.0.1
port: 8317
tls:
  enable: true
auth-dir: /tmp/runtime-auth
usage-statistics-enabled: false
plugins:
  dir: local-plugins
  enabled: true
debug: true
`))
	if errNormalize != nil {
		t.Fatalf("normalizePersistentConfig() error = %v", errNormalize)
	}
	for _, removed := range []string{
		"host:",
		"port:",
		"tls:",
		"auth-dir:",
		"usage-statistics-enabled:",
		"dir:",
	} {
		if strings.Contains(normalized, removed) {
			t.Fatalf("normalized config contains local or deprecated field %q: %s", removed, normalized)
		}
	}
	if !strings.Contains(normalized, "enabled: true") || !strings.Contains(normalized, "debug: true") {
		t.Fatalf("normalized config lost runtime-managed fields: %s", normalized)
	}
}

func TestDefaultSystemConfigIsValid(t *testing.T) {
	cfg, errParse := config.ParseConfigBytes([]byte(defaultSystemConfig))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}
	if cfg.Host != "" || cfg.Port != 0 {
		t.Fatalf("default database config contains local listener settings: %+v", cfg)
	}
	if cfg.AuthDir != "" {
		t.Fatalf("default database config auth dir = %q, want empty", cfg.AuthDir)
	}
}

func TestPostgresStoreCloseKeepsCallerManagedWorkspace(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "workspace")
	if errMkdir := os.MkdirAll(workspace, 0o700); errMkdir != nil {
		t.Fatalf("create workspace: %v", errMkdir)
	}
	store := &PostgresStore{spoolRoot: workspace}
	if errClose := store.Close(); errClose != nil {
		t.Fatalf("Close() error = %v", errClose)
	}
	if _, errStat := os.Stat(workspace); errStat != nil {
		t.Fatalf("caller-managed workspace was removed: %v", errStat)
	}
}

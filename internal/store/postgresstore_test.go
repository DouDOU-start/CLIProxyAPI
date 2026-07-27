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

func TestNormalizePersistentConfigClearsRuntimeAuthDir(t *testing.T) {
	normalized, errNormalize := normalizePersistentConfig([]byte("port: 8317\nauth-dir: /tmp/runtime-auth\ndebug: true\n"))
	if errNormalize != nil {
		t.Fatalf("normalizePersistentConfig() error = %v", errNormalize)
	}
	if strings.Contains(normalized, "/tmp/runtime-auth") {
		t.Fatalf("normalized config contains runtime auth directory: %s", normalized)
	}
	if !strings.Contains(normalized, "port: 8317") || !strings.Contains(normalized, "debug: true") {
		t.Fatalf("normalized config lost system fields: %s", normalized)
	}
}

func TestDefaultSystemConfigIsValid(t *testing.T) {
	cfg, errParse := config.ParseConfigBytes([]byte(defaultSystemConfig))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}
	if cfg.Port != 8317 {
		t.Fatalf("default system config port = %d, want 8317", cfg.Port)
	}
	if cfg.AuthDir != "" {
		t.Fatalf("default system config auth dir = %q, want empty", cfg.AuthDir)
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

package store

import (
	"os"
	"path/filepath"
	"testing"
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

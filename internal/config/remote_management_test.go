package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigPreservesRemoteManagementPassword(t *testing.T) {
	const password = "secret-123"
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	payload := "remote-management:\n  email: admin@example.com\n  password: " + password + "\n"
	if errWrite := os.WriteFile(configPath, []byte(payload), 0o600); errWrite != nil {
		t.Fatalf("write config: %v", errWrite)
	}

	cfg, errLoad := LoadConfigOptional(configPath, false)
	if errLoad != nil {
		t.Fatalf("LoadConfigOptional() error = %v", errLoad)
	}
	if cfg.RemoteManagement.Email != "admin@example.com" {
		t.Fatalf("email = %q, want admin@example.com", cfg.RemoteManagement.Email)
	}
	if cfg.RemoteManagement.Password != password {
		t.Fatalf("password = %q, want plaintext password", cfg.RemoteManagement.Password)
	}

	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read persisted config: %v", errRead)
	}
	if !strings.Contains(string(persisted), password) {
		t.Fatal("persisted config no longer contains the plaintext management password")
	}
}

func TestParseConfigBytesValidatesRemoteManagementCredentials(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		wantErr string
	}{
		{
			name:    "email without password",
			payload: "remote-management:\n  email: admin@example.com\n",
			wantErr: "must be configured together",
		},
		{
			name:    "invalid email",
			payload: "remote-management:\n  email: invalid\n  password: test-password-123\n",
			wantErr: "email is invalid",
		},
		{
			name:    "short password",
			payload: "remote-management:\n  email: admin@example.com\n  password: short\n",
			wantErr: "at least 8 characters",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, errParse := ParseConfigBytes([]byte(test.payload))
			if errParse == nil || !strings.Contains(errParse.Error(), test.wantErr) {
				t.Fatalf("ParseConfigBytes() error = %v, want containing %q", errParse, test.wantErr)
			}
		})
	}
}

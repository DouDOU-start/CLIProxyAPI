package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigBytesAlwaysEnablesUsageStatistics(t *testing.T) {
	cfg, errParse := ParseConfigBytes([]byte("usage-statistics-enabled: false\n"))
	if errParse != nil {
		t.Fatalf("ParseConfigBytes() error = %v", errParse)
	}
	if !cfg.UsageStatisticsEnabled {
		t.Fatal("usage statistics must remain enabled")
	}
}

func TestLoadConfigOptionalAlwaysEnablesUsageStatistics(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if errWrite := os.WriteFile(configPath, []byte("usage-statistics-enabled: false\n"), 0o600); errWrite != nil {
		t.Fatalf("write config: %v", errWrite)
	}
	cfg, errLoad := LoadConfigOptional(configPath, false)
	if errLoad != nil {
		t.Fatalf("LoadConfigOptional() error = %v", errLoad)
	}
	if !cfg.UsageStatisticsEnabled {
		t.Fatal("usage statistics must remain enabled")
	}
	if errSave := SaveConfigPreserveComments(configPath, cfg); errSave != nil {
		t.Fatalf("SaveConfigPreserveComments() error = %v", errSave)
	}
	saved, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read saved config: %v", errRead)
	}
	if strings.Contains(string(saved), "usage-statistics-enabled") {
		t.Fatal("deprecated usage statistics switch was retained")
	}
}

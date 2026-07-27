package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestLoadLocalStartupConfig(t *testing.T) {
	tests := []struct {
		name       string
		file       string
		wantHost   string
		wantPort   int
		wantDSN    string
		wantSchema string
		wantPlugin string
		wantErr    bool
	}{
		{
			name:       "YAML configuration",
			file:       "host: 127.0.0.1\nport: 9443\nplugins:\n  dir: local-plugins\npostgresql:\n  dsn: postgres://yaml\n  schema: app\n",
			wantHost:   "127.0.0.1",
			wantPort:   9443,
			wantDSN:    "postgres://yaml",
			wantSchema: "app",
			wantPlugin: "local-plugins",
		},
		{name: "startup defaults", file: "postgresql:\n  dsn: postgres://yaml\n", wantPort: 8317, wantDSN: "postgres://yaml", wantPlugin: "plugins"},
		{name: "missing file", wantErr: true},
		{name: "missing DSN", file: "postgresql:\n  schema: public\n", wantErr: true},
		{name: "invalid port", file: "port: 70000\npostgresql:\n  dsn: postgres://yaml\n", wantErr: true},
		{name: "invalid YAML", file: "postgresql: [", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			if test.file != "" {
				if errWrite := os.WriteFile(path, []byte(test.file), 0o600); errWrite != nil {
					t.Fatalf("write bootstrap config: %v", errWrite)
				}
			}
			got, errConfig := loadLocalStartupConfig(path)
			if (errConfig != nil) != test.wantErr {
				t.Fatalf("loadLocalStartupConfig() error = %v, wantErr %t", errConfig, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if got.Host != test.wantHost || got.Port != test.wantPort || got.PostgreSQL.DSN != test.wantDSN || got.PostgreSQL.Schema != test.wantSchema || got.Plugins.Dir != test.wantPlugin {
				t.Fatalf("loadLocalStartupConfig() = %+v, want host=%q port=%d dsn=%q schema=%q plugins.dir=%q", got, test.wantHost, test.wantPort, test.wantDSN, test.wantSchema, test.wantPlugin)
			}
		})
	}
}

func TestApplyLocalStartupConfig(t *testing.T) {
	startup := &localStartupConfig{Host: "127.0.0.1", Port: 9443}
	startup.Plugins.Dir = "local-plugins"
	runtimeCfg := &config.Config{}
	runtimeCfg.Plugins.Enabled = true

	applyLocalStartupConfig(runtimeCfg, startup)

	if runtimeCfg.Host != startup.Host || runtimeCfg.Port != startup.Port {
		t.Fatalf("local listener settings were not applied: %+v", runtimeCfg)
	}
	if runtimeCfg.Plugins.Dir != startup.Plugins.Dir || !runtimeCfg.Plugins.Enabled {
		t.Fatalf("local plugin directory overlay lost runtime plugin settings: %+v", runtimeCfg.Plugins)
	}
}

func TestDatabaseBootstrapConfigPath(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		fallback string
		want     string
		wantErr  bool
	}{
		{name: "default", fallback: "config.yaml", want: "config.yaml"},
		{name: "short flag", args: []string{"-config", "custom.yaml"}, want: "custom.yaml"},
		{name: "long flag", args: []string{"--config", "custom.yaml"}, want: "custom.yaml"},
		{name: "short assignment", args: []string{"-config=custom.yaml"}, want: "custom.yaml"},
		{name: "long assignment", args: []string{"--config=custom.yaml"}, want: "custom.yaml"},
		{name: "after separator ignored", args: []string{"--", "--config", "custom.yaml"}, fallback: "config.yaml", want: "config.yaml"},
		{name: "missing value", args: []string{"--config"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, errPath := databaseBootstrapConfigPath(test.args, test.fallback)
			if (errPath != nil) != test.wantErr {
				t.Fatalf("databaseBootstrapConfigPath() error = %v, wantErr %t", errPath, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("databaseBootstrapConfigPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveDatabaseBootstrapConfigPath(t *testing.T) {
	workingDirectory := filepath.Join(string(filepath.Separator), "workspace")
	if got := resolveDatabaseBootstrapConfigPath("", workingDirectory); got != filepath.Join(workingDirectory, "config.yaml") {
		t.Fatalf("empty path resolved to %q", got)
	}
	if got := resolveDatabaseBootstrapConfigPath("custom.yaml", workingDirectory); got != filepath.Join(workingDirectory, "custom.yaml") {
		t.Fatalf("relative path resolved to %q", got)
	}
}

func TestShouldEnableExampleAPIKeySafeMode(t *testing.T) {
	cfgWithExampleKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key", " your-api-key-1 "},
		},
	}
	cfgWithRealKey := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []string{"real-key"},
		},
	}

	tests := []struct {
		name               string
		cfg                *config.Config
		commandMode        bool
		cloudConfigMissing bool
		homeMode           bool
		want               bool
	}{
		{
			name: "normal server with example key",
			cfg:  cfgWithExampleKey,
			want: true,
		},
		{
			name:        "one-shot command is not blocked",
			cfg:         cfgWithExampleKey,
			commandMode: true,
			want:        false,
		},
		{
			name:     "home mode is not blocked",
			cfg:      cfgWithExampleKey,
			homeMode: true,
			want:     false,
		},
		{
			name:               "cloud standby without config is not blocked",
			cfg:                cfgWithExampleKey,
			cloudConfigMissing: true,
			want:               false,
		},
		{
			name: "normal server with real key",
			cfg:  cfgWithRealKey,
			want: false,
		},
		{
			name: "nil config",
			cfg:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldEnableExampleAPIKeySafeMode(tt.cfg, tt.commandMode, tt.cloudConfigMissing, tt.homeMode)
			if got != tt.want {
				t.Fatalf("shouldEnableExampleAPIKeySafeMode() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestModelCatalogUpdaterPlan(t *testing.T) {
	tests := []struct {
		name            string
		localModel      bool
		homeEnabled     bool
		wantModels      bool
		wantCodexClient bool
	}{
		{
			name:            "normal CPA refreshes both catalogs",
			localModel:      false,
			homeEnabled:     false,
			wantModels:      true,
			wantCodexClient: true,
		},
		{
			name:            "home mode keeps models.json local and refreshes codex templates",
			localModel:      false,
			homeEnabled:     true,
			wantModels:      false,
			wantCodexClient: true,
		},
		{
			name:            "local-model disables both remote catalogs",
			localModel:      true,
			homeEnabled:     false,
			wantModels:      false,
			wantCodexClient: false,
		},
		{
			name:            "local-model disables both remote catalogs even under home",
			localModel:      true,
			homeEnabled:     true,
			wantModels:      false,
			wantCodexClient: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModels, gotCodex := modelCatalogUpdaterPlan(tt.localModel, tt.homeEnabled)
			if gotModels != tt.wantModels || gotCodex != tt.wantCodexClient {
				t.Fatalf("modelCatalogUpdaterPlan(%v, %v) = (%v, %v), want (%v, %v)",
					tt.localModel, tt.homeEnabled, gotModels, gotCodex, tt.wantModels, tt.wantCodexClient)
			}
		})
	}
}

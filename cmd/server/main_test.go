package main

import (
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

func TestRequiredPostgresConfig(t *testing.T) {
	tests := []struct {
		name       string
		values     map[string]string
		wantDSN    string
		wantSchema string
		wantErr    bool
	}{
		{name: "missing DSN", values: map[string]string{}, wantErr: true},
		{name: "uppercase variables", values: map[string]string{"PGSTORE_DSN": "postgres://db", "PGSTORE_SCHEMA": "cliproxy"}, wantDSN: "postgres://db", wantSchema: "cliproxy"},
		{name: "lowercase compatibility", values: map[string]string{"pgstore_dsn": "postgres://lower", "pgstore_schema": "public"}, wantDSN: "postgres://lower", wantSchema: "public"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			lookup := func(keys ...string) (string, bool) {
				for _, key := range keys {
					value := strings.TrimSpace(test.values[key])
					if value != "" {
						return value, true
					}
				}
				return "", false
			}
			gotDSN, gotSchema, errConfig := requiredPostgresConfig(lookup)
			if (errConfig != nil) != test.wantErr {
				t.Fatalf("requiredPostgresConfig() error = %v, wantErr %t", errConfig, test.wantErr)
			}
			if gotDSN != test.wantDSN || gotSchema != test.wantSchema {
				t.Fatalf("requiredPostgresConfig() = (%q, %q), want (%q, %q)", gotDSN, gotSchema, test.wantDSN, test.wantSchema)
			}
		})
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

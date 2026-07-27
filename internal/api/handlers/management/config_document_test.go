package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"gopkg.in/yaml.v3"
)

func TestGetConfigReturnsPersistedConfigurationAsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	content := "debug: true\nremote-management:\n  email: admin@example.com\n  password: test-password\n"
	if errWrite := os.WriteFile(configPath, []byte(content), 0o600); errWrite != nil {
		t.Fatalf("write config: %v", errWrite)
	}
	h := &Handler{configFilePath: configPath}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)

	h.GetConfig(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(recorder.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode response: %v", errDecode)
	}
	if payload["debug"] != true {
		t.Fatalf("debug = %#v, want true", payload["debug"])
	}
	remote, ok := payload["remote-management"].(map[string]any)
	if !ok || remote["email"] != "admin@example.com" {
		t.Fatalf("remote-management = %#v", payload["remote-management"])
	}
}

func TestPutConfigAcceptsJSONAndPersistsConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if errWrite := os.WriteFile(configPath, []byte("debug: false\n"), 0o600); errWrite != nil {
		t.Fatalf("write config: %v", errWrite)
	}
	h := &Handler{cfg: &config.Config{}, configFilePath: configPath}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body := `{"debug":true,"remote-management":{"email":"admin@example.com","password":"test-password"}}`
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutConfig(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if h.cfg == nil || !h.cfg.Debug || h.cfg.RemoteManagement.Email != "admin@example.com" {
		t.Fatalf("handler config was not reloaded: %#v", h.cfg)
	}
	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read persisted config: %v", errRead)
	}
	var decoded map[string]any
	if errDecode := yaml.Unmarshal(persisted, &decoded); errDecode != nil {
		t.Fatalf("decode persisted config: %v", errDecode)
	}
	if decoded["debug"] != true {
		t.Fatalf("persisted debug = %#v, want true", decoded["debug"])
	}
}

func TestPutConfigRejectsNonObjectJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{cfg: &config.Config{}, configFilePath: filepath.Join(t.TempDir(), "config.yaml")}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPut, "/v0/management/config", strings.NewReader("[]"))
	ctx.Request.Header.Set("Content-Type", "application/json")

	h.PutConfig(ctx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}

package management

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"golang.org/x/crypto/bcrypt"
)

type sessionLoginResponse struct {
	Authenticated bool   `json:"authenticated"`
	Email         string `json:"email"`
	CSRFToken     string `json:"csrf_token"`
}

type setupStatusResponse struct {
	Required     bool `json:"required"`
	RemoteClient bool `json:"remote_client"`
}

func newManagementSetupTestHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if errWrite := os.WriteFile(configPath, []byte("host: \"\"\nport: 8317\nauth-dir: \"\"\n"), 0o600); errWrite != nil {
		t.Fatalf("write setup config: %v", errWrite)
	}
	return &Handler{
		cfg:            &config.Config{Port: 8317},
		configFilePath: configPath,
		failedAttempts: make(map[string]*attemptInfo),
		sessions:       make(map[string]*managementSession),
	}, configPath
}

func TestManagementFirstRunSetupCreatesAdministrator(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, configPath := newManagementSetupTestHandler(t)
	persistCalls := 0
	h.SetConfigPersistHook(func(context.Context) error {
		persistCalls++
		return nil
	})
	engine := gin.New()
	engine.GET("/v0/management/auth/setup", h.GetSetupStatus)
	engine.POST("/v0/management/auth/setup", h.Setup)
	engine.POST("/v0/management/auth/login", h.Login)

	statusReq := httptest.NewRequest(http.MethodGet, "/v0/management/auth/setup", nil)
	statusReq.RemoteAddr = "192.0.2.10:12345"
	statusRec := httptest.NewRecorder()
	engine.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("setup status = %d, want %d body=%s", statusRec.Code, http.StatusOK, statusRec.Body.String())
	}
	var setupStatus setupStatusResponse
	if errDecode := json.Unmarshal(statusRec.Body.Bytes(), &setupStatus); errDecode != nil {
		t.Fatalf("decode setup status: %v", errDecode)
	}
	if !setupStatus.Required || !setupStatus.RemoteClient {
		t.Fatalf("unexpected setup status: %#v", setupStatus)
	}

	setupBody := `{"email":"Admin@Example.com","password":"test-password-123","confirm_password":"test-password-123","allow_remote":false}`
	setupReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth/setup", strings.NewReader(setupBody))
	setupReq.RemoteAddr = "192.0.2.10:12345"
	setupReq.Header.Set("Content-Type", "application/json")
	setupReq.Header.Set("Origin", "http://example.com")
	setupRec := httptest.NewRecorder()
	engine.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusCreated {
		t.Fatalf("setup status = %d, want %d body=%s", setupRec.Code, http.StatusCreated, setupRec.Body.String())
	}
	if persistCalls != 1 {
		t.Fatalf("persist calls = %d, want 1", persistCalls)
	}
	if h.cfg.RemoteManagement.Email != "admin@example.com" || !h.cfg.RemoteManagement.AllowRemote {
		t.Fatalf("unexpected management config: %#v", h.cfg.RemoteManagement)
	}
	if !looksLikeBcryptPassword(h.cfg.RemoteManagement.Password) {
		t.Fatalf("setup password is not bcrypt: %q", h.cfg.RemoteManagement.Password)
	}
	if !managementPasswordMatches(managementCredentials{
		Email:       h.cfg.RemoteManagement.Email,
		Password:    h.cfg.RemoteManagement.Password,
		PasswordRaw: false,
	}, "admin@example.com", "test-password-123") {
		t.Fatal("created administrator password does not match")
	}
	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read persisted setup config: %v", errRead)
	}
	if strings.Contains(string(persisted), "test-password-123") {
		t.Fatal("persisted setup config contains the plaintext password")
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"test-password-123"}`))
	loginReq.RemoteAddr = "192.0.2.10:12345"
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	engine.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("login after setup status = %d, want %d body=%s", loginRec.Code, http.StatusOK, loginRec.Body.String())
	}

	secondSetupReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth/setup", strings.NewReader(setupBody))
	secondSetupReq.RemoteAddr = "192.0.2.10:12345"
	secondSetupReq.Header.Set("Content-Type", "application/json")
	secondSetupRec := httptest.NewRecorder()
	engine.ServeHTTP(secondSetupRec, secondSetupReq)
	if secondSetupRec.Code != http.StatusConflict {
		t.Fatalf("second setup status = %d, want %d", secondSetupRec.Code, http.StatusConflict)
	}
}

func TestManagementFirstRunSetupRollsBackOnPersistenceFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, configPath := newManagementSetupTestHandler(t)
	h.SetConfigPersistHook(func(context.Context) error { return errors.New("database unavailable") })
	engine := gin.New()
	engine.POST("/v0/management/auth/setup", h.Setup)

	setupReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth/setup", strings.NewReader(`{"email":"admin@example.com","password":"test-password-123","confirm_password":"test-password-123"}`))
	setupReq.RemoteAddr = "127.0.0.1:12345"
	setupReq.Header.Set("Content-Type", "application/json")
	setupRec := httptest.NewRecorder()
	engine.ServeHTTP(setupRec, setupReq)
	if setupRec.Code != http.StatusInternalServerError {
		t.Fatalf("setup status = %d, want %d body=%s", setupRec.Code, http.StatusInternalServerError, setupRec.Body.String())
	}
	if h.cfg.RemoteManagement.Email != "" || h.cfg.RemoteManagement.Password != "" {
		t.Fatalf("management config was not rolled back: %#v", h.cfg.RemoteManagement)
	}
	persisted, errRead := os.ReadFile(configPath)
	if errRead != nil {
		t.Fatalf("read rolled back config: %v", errRead)
	}
	if strings.Contains(string(persisted), "admin@example.com") {
		t.Fatal("rolled back config still contains the administrator email")
	}
}

func TestManagementFirstRunSetupRejectsCrossOriginRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _ := newManagementSetupTestHandler(t)
	engine := gin.New()
	engine.POST("/v0/management/auth/setup", h.Setup)

	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth/setup", strings.NewReader(`{"email":"admin@example.com","password":"test-password-123","confirm_password":"test-password-123"}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://attacker.example")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin setup status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestManagementPasswordMatchesPlaintextConfig(t *testing.T) {
	h := &Handler{cfg: &config.Config{RemoteManagement: config.RemoteManagement{
		Email:    "admin@example.com",
		Password: "secret-123",
	}}}
	credentials, configured := h.configuredManagementCredentials()
	if !configured {
		t.Fatal("expected plaintext management credentials to be configured")
	}
	if !credentials.PasswordRaw {
		t.Fatal("expected plaintext config password to use raw comparison")
	}
	if !managementPasswordMatches(credentials, "admin@example.com", "secret-123") {
		t.Fatal("expected plaintext management password to match")
	}
}

func TestManagementPasswordMatchesLegacyBcryptConfig(t *testing.T) {
	hashed, errHash := bcrypt.GenerateFromPassword([]byte("secret-123"), bcrypt.MinCost)
	if errHash != nil {
		t.Fatalf("hash legacy management password: %v", errHash)
	}
	h := &Handler{cfg: &config.Config{RemoteManagement: config.RemoteManagement{
		Email:    "admin@example.com",
		Password: string(hashed),
	}}}
	credentials, configured := h.configuredManagementCredentials()
	if !configured {
		t.Fatal("expected legacy management credentials to be configured")
	}
	if credentials.PasswordRaw {
		t.Fatal("expected legacy bcrypt password to use hash comparison")
	}
	if !managementPasswordMatches(credentials, "admin@example.com", "secret-123") {
		t.Fatal("expected legacy bcrypt management password to match")
	}
}

func performSessionLogin(t *testing.T, engine *gin.Engine) (*http.Cookie, sessionLoginResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v0/management/auth/login", strings.NewReader(`{"email":"admin@example.com","password":"test-password-123"}`))
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response sessionLoginResponse
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &response); errDecode != nil {
		t.Fatalf("decode login response: %v", errDecode)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	return cookies[0], response
}

func TestManagementSessionLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newSessionTestHandler()
	engine := gin.New()
	engine.POST("/v0/management/auth/login", h.Login)
	engine.GET("/v0/management/auth/session", h.GetSession)
	protected := engine.Group("/v0/management")
	protected.Use(h.Middleware())
	protected.GET("/config", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	protected.POST("/auth/logout", h.Logout)

	cookie, loginResponse := performSessionLogin(t, engine)
	if !loginResponse.Authenticated || loginResponse.Email != "admin@example.com" || loginResponse.CSRFToken == "" {
		t.Fatalf("unexpected login response: %#v", loginResponse)
	}
	if !cookie.HttpOnly || cookie.Secure || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unexpected session cookie flags: %#v", cookie)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/v0/management/auth/session", nil)
	sessionReq.RemoteAddr = "127.0.0.1:12345"
	sessionReq.AddCookie(cookie)
	sessionRec := httptest.NewRecorder()
	engine.ServeHTTP(sessionRec, sessionReq)
	if sessionRec.Code != http.StatusOK {
		t.Fatalf("session status = %d, want %d body=%s", sessionRec.Code, http.StatusOK, sessionRec.Body.String())
	}

	missingCSRFReq := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	missingCSRFReq.RemoteAddr = "127.0.0.1:12345"
	missingCSRFReq.AddCookie(cookie)
	missingCSRFRec := httptest.NewRecorder()
	engine.ServeHTTP(missingCSRFRec, missingCSRFReq)
	if missingCSRFRec.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status = %d, want %d", missingCSRFRec.Code, http.StatusForbidden)
	}

	protectedReq := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
	protectedReq.RemoteAddr = "127.0.0.1:12345"
	protectedReq.AddCookie(cookie)
	protectedReq.Header.Set("X-CSRF-Token", loginResponse.CSRFToken)
	protectedRec := httptest.NewRecorder()
	engine.ServeHTTP(protectedRec, protectedReq)
	if protectedRec.Code != http.StatusNoContent {
		t.Fatalf("protected status = %d, want %d body=%s", protectedRec.Code, http.StatusNoContent, protectedRec.Body.String())
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/v0/management/auth/logout", nil)
	logoutReq.RemoteAddr = "127.0.0.1:12345"
	logoutReq.AddCookie(cookie)
	logoutReq.Header.Set("X-CSRF-Token", loginResponse.CSRFToken)
	logoutRec := httptest.NewRecorder()
	engine.ServeHTTP(logoutRec, logoutReq)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout status = %d, want %d body=%s", logoutRec.Code, http.StatusOK, logoutRec.Body.String())
	}

	expiredReq := httptest.NewRequest(http.MethodGet, "/v0/management/auth/session", nil)
	expiredReq.RemoteAddr = "127.0.0.1:12345"
	expiredReq.AddCookie(cookie)
	expiredRec := httptest.NewRecorder()
	engine.ServeHTTP(expiredRec, expiredReq)
	if expiredRec.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d, want %d", expiredRec.Code, http.StatusUnauthorized)
	}
}

func TestManagementSessionInvalidatedAfterCredentialChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newSessionTestHandler()
	engine := gin.New()
	engine.POST("/v0/management/auth/login", h.Login)
	engine.GET("/v0/management/auth/session", h.GetSession)

	cookie, _ := performSessionLogin(t, engine)
	h.envPassword = "changed-password-456"

	req := httptest.NewRequest(http.MethodGet, "/v0/management/auth/session", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("credential change status = %d, want %d body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
	}
}

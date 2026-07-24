package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

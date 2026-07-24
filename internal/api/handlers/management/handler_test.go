package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
)

func newSessionTestHandler() *Handler {
	return &Handler{
		cfg:            &config.Config{},
		failedAttempts: make(map[string]*attemptInfo),
		sessions:       make(map[string]*managementSession),
		envEmail:       "admin@example.com",
		envPassword:    "test-password-123",
	}
}

func TestAuthenticateManagementCredentialsLocalhostBanBlocksValidLogin(t *testing.T) {
	h := newSessionTestHandler()

	for i := 0; i < managementMaxFailures; i++ {
		allowed, statusCode, errMsg := h.AuthenticateManagementCredentials("127.0.0.1", true, "admin@example.com", "wrong-password")
		if allowed {
			t.Fatalf("expected auth to be denied at attempt %d", i+1)
		}
		if statusCode != http.StatusUnauthorized || errMsg != "invalid email or password" {
			t.Fatalf("unexpected auth failure at attempt %d: status=%d msg=%q", i+1, statusCode, errMsg)
		}
	}

	allowed, statusCode, errMsg := h.AuthenticateManagementCredentials("127.0.0.1", true, "admin@example.com", "test-password-123")
	if allowed {
		t.Fatal("expected valid credentials to be denied while blocked")
	}
	if statusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", statusCode, http.StatusTooManyRequests)
	}
	if !strings.HasPrefix(errMsg, "too many failed attempts; try again in") {
		t.Fatalf("unexpected blocked message: %q", errMsg)
	}
}

func TestMiddlewareSetsSupportPluginHeader(t *testing.T) {
	h := newSessionTestHandler()
	middleware := h.Middleware()

	t.Run("missing session", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		c.Request.RemoteAddr = "127.0.0.1:12345"

		middleware(c)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if got := rec.Header().Get("X-CPA-SUPPORT-PLUGIN"); got != pluginhost.SupportPluginHeaderValue() {
			t.Fatalf("X-CPA-SUPPORT-PLUGIN = %q, want %q", got, pluginhost.SupportPluginHeaderValue())
		}
	})

	t.Run("valid session", func(t *testing.T) {
		credentials, configured := h.configuredManagementCredentials()
		if !configured {
			t.Fatal("expected management credentials to be configured")
		}
		const sessionID = "test-session"
		const csrfToken = "test-csrf-token"
		h.storeManagementSession(sessionID, &managementSession{
			Email:                 credentials.Email,
			CSRFToken:             csrfToken,
			CredentialFingerprint: credentials.Fingerprint,
			CreatedAt:             time.Now(),
			ExpiresAt:             time.Now().Add(time.Hour),
		})

		engine := gin.New()
		engine.GET("/v0/management/config", middleware, func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.AddCookie(&http.Cookie{Name: managementSessionCookie, Value: sessionID})
		req.Header.Set("X-CSRF-Token", csrfToken)
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("X-CPA-SUPPORT-PLUGIN"); got != pluginhost.SupportPluginHeaderValue() {
			t.Fatalf("X-CPA-SUPPORT-PLUGIN = %q, want %q", got, pluginhost.SupportPluginHeaderValue())
		}
	})
}

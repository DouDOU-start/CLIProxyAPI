package management

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const (
	managementSessionCookie   = "cpa_management_session"
	managementSessionContext  = "cpa_management_session_id"
	managementSessionDuration = 12 * time.Hour
	managementRememberedTTL   = 30 * 24 * time.Hour
	managementMaxSessions     = 1024
	managementMaxFailures     = 5
	managementBanDuration     = 30 * time.Minute
)

type managementSession struct {
	Email                 string
	CSRFToken             string
	CredentialFingerprint string
	CreatedAt             time.Time
	ExpiresAt             time.Time
}

type managementCredentials struct {
	Email       string
	Password    string
	PasswordRaw bool
	AllowRemote bool
	Fingerprint string
}

func (h *Handler) configuredManagementCredentials() (managementCredentials, bool) {
	if h == nil {
		return managementCredentials{}, false
	}

	h.mu.Lock()
	var credentials managementCredentials
	if h.cfg != nil {
		credentials.Email = strings.ToLower(strings.TrimSpace(h.cfg.RemoteManagement.Email))
		credentials.Password = strings.TrimSpace(h.cfg.RemoteManagement.Password)
		credentials.PasswordRaw = !looksLikeBcryptPassword(credentials.Password)
		credentials.AllowRemote = h.cfg.RemoteManagement.AllowRemote
	}
	h.mu.Unlock()

	if h.envEmail != "" {
		credentials.Email = h.envEmail
	}
	if h.envPassword != "" {
		credentials.Password = h.envPassword
		credentials.PasswordRaw = true
	}
	if credentials.Email == "" || credentials.Password == "" {
		return credentials, false
	}

	sum := sha256.Sum256([]byte(credentials.Email + "\x00" + credentials.Password))
	credentials.Fingerprint = base64.RawURLEncoding.EncodeToString(sum[:])
	return credentials, true
}

func (h *Handler) Login(c *gin.Context) {
	if h == nil || c == nil {
		return
	}
	c.Header("Cache-Control", "no-store")
	clientIP := c.ClientIP()
	credentials, configured := h.configuredManagementCredentials()
	if !isLocalManagementClient(clientIP) && !credentials.AllowRemote {
		c.JSON(http.StatusForbidden, gin.H{"error": "remote_management_disabled", "message": "remote management is disabled"})
		return
	}
	if !configured {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "management_credentials_not_configured", "message": "management email and password are not configured"})
		return
	}
	if remaining, blocked := h.loginBlockedFor(clientIP); blocked {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "too_many_login_attempts", "message": fmt.Sprintf("too many failed attempts; try again in %s", remaining)})
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Remember bool   `json:"remember"`
	}
	if errBind := c.ShouldBindJSON(&body); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": "email and password are required"})
		return
	}
	body.Email = strings.ToLower(strings.TrimSpace(body.Email))
	if len(body.Email) > 320 || len(body.Password) > 1024 || !managementPasswordMatches(credentials, body.Email, body.Password) {
		h.recordLoginFailure(clientIP)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid_credentials", "message": "invalid email or password"})
		return
	}

	h.resetLoginFailures(clientIP)
	ttl := managementSessionDuration
	if body.Remember {
		ttl = managementRememberedTTL
	}
	sessionID, errSessionID := secureRandomToken(32)
	csrfToken, errCSRF := secureRandomToken(32)
	if errSessionID != nil || errCSRF != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "session_creation_failed", "message": "failed to create management session"})
		return
	}

	now := time.Now()
	session := &managementSession{
		Email:                 credentials.Email,
		CSRFToken:             csrfToken,
		CredentialFingerprint: credentials.Fingerprint,
		CreatedAt:             now,
		ExpiresAt:             now.Add(ttl),
	}
	h.storeManagementSession(sessionID, session)
	h.setManagementSessionCookie(c, sessionID, session.ExpiresAt, body.Remember)
	c.JSON(http.StatusOK, managementSessionResponse(session))
}

// AuthenticateManagementCredentials verifies management credentials for non-HTTP transports.
func (h *Handler) AuthenticateManagementCredentials(clientIP string, localClient bool, email, password string) (bool, int, string) {
	credentials, configured := h.configuredManagementCredentials()
	if !localClient && !credentials.AllowRemote {
		return false, http.StatusForbidden, "remote management is disabled"
	}
	if !configured {
		return false, http.StatusServiceUnavailable, "management email and password are not configured"
	}
	if remaining, blocked := h.loginBlockedFor(clientIP); blocked {
		return false, http.StatusTooManyRequests, fmt.Sprintf("too many failed attempts; try again in %s", remaining)
	}

	email = strings.ToLower(strings.TrimSpace(email))
	if len(email) > 320 || len(password) > 1024 || !managementPasswordMatches(credentials, email, password) {
		h.recordLoginFailure(clientIP)
		return false, http.StatusUnauthorized, "invalid email or password"
	}

	h.resetLoginFailures(clientIP)
	return true, http.StatusOK, ""
}

func (h *Handler) GetSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	session, ok := h.authenticateSession(c, false)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, managementSessionResponse(session))
}

func (h *Handler) Logout(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if sessionID, exists := c.Get(managementSessionContext); exists {
		if id, ok := sessionID.(string); ok && id != "" {
			h.sessionsMu.Lock()
			delete(h.sessions, id)
			h.sessionsMu.Unlock()
		}
	}
	h.clearManagementSessionCookie(c)
	c.JSON(http.StatusOK, gin.H{"authenticated": false})
}

func (h *Handler) authenticateSession(c *gin.Context, requireCSRF bool) (*managementSession, bool) {
	if h == nil || c == nil {
		return nil, false
	}
	clientIP := c.ClientIP()
	credentials, configured := h.configuredManagementCredentials()
	if !isLocalManagementClient(clientIP) && !credentials.AllowRemote {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "remote_management_disabled", "message": "remote management is disabled"})
		return nil, false
	}
	if !configured {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "management_credentials_not_configured", "message": "management email and password are not configured"})
		return nil, false
	}

	sessionID, errCookie := c.Cookie(managementSessionCookie)
	if errCookie != nil || strings.TrimSpace(sessionID) == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session_required", "message": "management session is required"})
		return nil, false
	}

	now := time.Now()
	h.sessionsMu.Lock()
	session := h.sessions[sessionID]
	if session == nil || !session.ExpiresAt.After(now) || subtle.ConstantTimeCompare([]byte(session.CredentialFingerprint), []byte(credentials.Fingerprint)) != 1 {
		delete(h.sessions, sessionID)
		h.sessionsMu.Unlock()
		h.clearManagementSessionCookie(c)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "session_expired", "message": "management session has expired"})
		return nil, false
	}
	copySession := *session
	h.sessionsMu.Unlock()

	if requireCSRF {
		providedCSRF := strings.TrimSpace(c.GetHeader("X-CSRF-Token"))
		if providedCSRF == "" || subtle.ConstantTimeCompare([]byte(providedCSRF), []byte(copySession.CSRFToken)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "invalid_csrf_token", "message": "valid CSRF token is required"})
			return nil, false
		}
	}
	c.Set(managementSessionContext, sessionID)
	return &copySession, true
}

func managementPasswordMatches(credentials managementCredentials, email, password string) bool {
	emailMatches := subtle.ConstantTimeCompare([]byte(credentials.Email), []byte(email)) == 1
	if credentials.PasswordRaw {
		passwordMatches := subtle.ConstantTimeCompare([]byte(credentials.Password), []byte(password)) == 1
		return emailMatches && passwordMatches
	}
	passwordMatches := bcrypt.CompareHashAndPassword([]byte(credentials.Password), []byte(password)) == nil
	return emailMatches && passwordMatches
}

func looksLikeBcryptPassword(password string) bool {
	return len(password) > 4 && (password[:4] == "$2a$" || password[:4] == "$2b$" || password[:4] == "$2y$")
}

func managementSessionResponse(session *managementSession) gin.H {
	return gin.H{
		"authenticated": true,
		"email":         session.Email,
		"csrf_token":    session.CSRFToken,
		"expires_at":    session.ExpiresAt.Format(time.RFC3339),
	}
}

func (h *Handler) storeManagementSession(sessionID string, session *managementSession) {
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	if len(h.sessions) >= managementMaxSessions {
		var oldestID string
		var oldestTime time.Time
		for id, candidate := range h.sessions {
			if oldestID == "" || candidate.CreatedAt.Before(oldestTime) {
				oldestID = id
				oldestTime = candidate.CreatedAt
			}
		}
		delete(h.sessions, oldestID)
	}
	h.sessions[sessionID] = session
}

func (h *Handler) purgeExpiredSessions() {
	if h == nil {
		return
	}
	now := time.Now()
	h.sessionsMu.Lock()
	defer h.sessionsMu.Unlock()
	for id, session := range h.sessions {
		if session == nil || !session.ExpiresAt.After(now) {
			delete(h.sessions, id)
		}
	}
}

func (h *Handler) loginBlockedFor(clientIP string) (time.Duration, bool) {
	now := time.Now()
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	attempt := h.failedAttempts[clientIP]
	if attempt == nil || attempt.blockedUntil.IsZero() {
		return 0, false
	}
	if now.Before(attempt.blockedUntil) {
		return attempt.blockedUntil.Sub(now).Round(time.Second), true
	}
	attempt.blockedUntil = time.Time{}
	attempt.count = 0
	return 0, false
}

func (h *Handler) recordLoginFailure(clientIP string) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	attempt := h.failedAttempts[clientIP]
	if attempt == nil {
		attempt = &attemptInfo{}
		h.failedAttempts[clientIP] = attempt
	}
	attempt.count++
	attempt.lastActivity = time.Now()
	if attempt.count >= managementMaxFailures {
		attempt.blockedUntil = time.Now().Add(managementBanDuration)
		attempt.count = 0
	}
}

func (h *Handler) resetLoginFailures(clientIP string) {
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	if attempt := h.failedAttempts[clientIP]; attempt != nil {
		attempt.count = 0
		attempt.blockedUntil = time.Time{}
		attempt.lastActivity = time.Now()
	}
}

func (h *Handler) setManagementSessionCookie(c *gin.Context, sessionID string, expiresAt time.Time, persistent bool) {
	maxAge := 0
	if persistent {
		maxAge = int(time.Until(expiresAt).Seconds())
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     managementSessionCookie,
		Value:    sessionID,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearManagementSessionCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     managementSessionCookie,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
	})
}

func isLocalManagementClient(clientIP string) bool {
	return clientIP == "127.0.0.1" || clientIP == "::1"
}

func secureRandomToken(size int) (string, error) {
	raw := make([]byte, size)
	if _, errRead := rand.Read(raw); errRead != nil {
		return "", errRead
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

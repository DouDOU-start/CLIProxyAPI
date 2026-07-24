// Package management provides the management API handlers and middleware
// for configuring the server and managing auth files.
package management

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginstore"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type attemptInfo struct {
	count        int
	blockedUntil time.Time
	lastActivity time.Time // track last activity for cleanup
}

// attemptCleanupInterval controls how often stale IP entries are purged
const attemptCleanupInterval = 1 * time.Hour

// attemptMaxIdleTime controls how long an IP can be idle before cleanup
const attemptMaxIdleTime = 2 * time.Hour

// Handler aggregates config reference, persistence path and helpers.
type Handler struct {
	cfg                     *config.Config
	configFilePath          string
	mu                      sync.Mutex
	reloadMu                sync.Mutex
	reloadGeneration        uint64
	appliedReloadGeneration uint64
	attemptsMu              sync.Mutex
	failedAttempts          map[string]*attemptInfo // keyed by client IP
	sessionsMu              sync.Mutex
	sessions                map[string]*managementSession
	authManager             *coreauth.Manager
	tokenStore              coreauth.Store
	envEmail                string
	envPassword             string
	logDir                  string
	postAuthHook            coreauth.PostAuthHook
	postAuthPersistHook     coreauth.PostAuthHook
	pluginHost              *pluginhost.Host
	configReloadHook        func(context.Context, *config.Config)
	pluginStoreRegistryURL  string
	pluginStoreHTTPClient   pluginstore.HTTPDoer
	pluginReleaseCacheMu    sync.Mutex
	pluginReleaseCache      map[string]pluginReleaseCacheEntry
}

type configReloadSnapshot struct {
	cfg        *config.Config
	generation uint64
}

// NewHandler creates a new management handler instance.
func NewHandler(cfg *config.Config, configFilePath string, manager *coreauth.Manager) *Handler {
	envEmail, _ := os.LookupEnv("MANAGEMENT_EMAIL")
	envPassword, _ := os.LookupEnv("MANAGEMENT_PASSWORD")

	h := &Handler{
		cfg:            cfg,
		configFilePath: configFilePath,
		failedAttempts: make(map[string]*attemptInfo),
		sessions:       make(map[string]*managementSession),
		authManager:    manager,
		tokenStore:     sdkAuth.GetTokenStore(),
		envEmail:       strings.ToLower(strings.TrimSpace(envEmail)),
		envPassword:    strings.TrimSpace(envPassword),
	}
	h.startAttemptCleanup()
	return h
}

// startAttemptCleanup launches a background goroutine that periodically
// removes stale IP entries from failedAttempts to prevent memory leaks.
func (h *Handler) startAttemptCleanup() {
	go func() {
		ticker := time.NewTicker(attemptCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			h.purgeStaleAttempts()
			h.purgeExpiredSessions()
		}
	}()
}

// purgeStaleAttempts removes IP entries that have been idle beyond attemptMaxIdleTime
// and whose ban (if any) has expired.
func (h *Handler) purgeStaleAttempts() {
	now := time.Now()
	h.attemptsMu.Lock()
	defer h.attemptsMu.Unlock()
	for ip, ai := range h.failedAttempts {
		// Skip if still banned
		if !ai.blockedUntil.IsZero() && now.Before(ai.blockedUntil) {
			continue
		}
		// Remove if idle too long
		if now.Sub(ai.lastActivity) > attemptMaxIdleTime {
			delete(h.failedAttempts, ip)
		}
	}
}

// NewHandler creates a new management handler instance.
func NewHandlerWithoutConfigFilePath(cfg *config.Config, manager *coreauth.Manager) *Handler {
	return NewHandler(cfg, "", manager)
}

// SetConfig updates the in-memory config reference when the server hot-reloads.
func (h *Handler) SetConfig(cfg *config.Config) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cfg = cfg
	h.mu.Unlock()
}

// SetAuthManager updates the auth manager reference used by management endpoints.
func (h *Handler) SetAuthManager(manager *coreauth.Manager) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.authManager = manager
	h.mu.Unlock()
}

// SetPluginHost updates the plugin host used by plugin-backed management endpoints.
func (h *Handler) SetPluginHost(host *pluginhost.Host) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.pluginHost = host
	h.mu.Unlock()
}

// SetConfigReloadHook updates the callback used after management saves config changes.
func (h *Handler) SetConfigReloadHook(hook func(context.Context, *config.Config)) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.configReloadHook = hook
	h.mu.Unlock()
}

// reloadSnapshotConfigLocked clones the runtime config and assigns a reload generation.
// Callers must hold h.mu.
func (h *Handler) reloadSnapshotConfigLocked() configReloadSnapshot {
	if h == nil || h.cfg == nil {
		return configReloadSnapshot{}
	}
	h.reloadGeneration++
	return configReloadSnapshot{
		cfg:        h.cfg.CloneForRuntime(),
		generation: h.reloadGeneration,
	}
}

// saveConfigAndSnapshotLocked saves h.cfg and returns a full runtime config snapshot.
// Callers must hold h.mu.
func (h *Handler) saveConfigAndSnapshotLocked(c *gin.Context) (configReloadSnapshot, bool) {
	if errSave := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); errSave != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save config: %v", errSave)})
		return configReloadSnapshot{}, false
	}
	return h.reloadSnapshotConfigLocked(), true
}

// reloadConfigAfterManagementSave reloads from an independent config snapshot.
// Callers must pass a full Config clone captured immediately after a successful save.
func (h *Handler) reloadConfigAfterManagementSave(ctx context.Context, snapshot configReloadSnapshot) {
	if h == nil || snapshot.cfg == nil || snapshot.generation == 0 {
		return
	}
	h.reloadMu.Lock()
	defer h.reloadMu.Unlock()

	h.mu.Lock()
	if snapshot.generation < h.appliedReloadGeneration {
		h.mu.Unlock()
		return
	}
	hook := h.configReloadHook
	host := h.pluginHost
	h.mu.Unlock()
	if hook != nil {
		hook(ctx, snapshot.cfg)
	} else if host != nil {
		host.ApplyConfig(ctx, snapshot.cfg)
	}

	h.mu.Lock()
	if snapshot.generation > h.appliedReloadGeneration {
		h.appliedReloadGeneration = snapshot.generation
	}
	h.mu.Unlock()
}

// reloadConfigAfterManagementSaveAsync reloads from an independent config snapshot.
// Callers must pass a full Config clone captured immediately after a successful save.
func (h *Handler) reloadConfigAfterManagementSaveAsync(ctx context.Context, snapshot configReloadSnapshot) {
	if h == nil || snapshot.cfg == nil || snapshot.generation == 0 {
		return
	}
	reloadCtx := context.Background()
	if ctx != nil {
		reloadCtx = context.WithoutCancel(ctx)
	}
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.WithField("panic", recovered).Error("management: async config reload panicked")
			}
		}()
		h.reloadConfigAfterManagementSave(reloadCtx, snapshot)
	}()
}

// SetLogDirectory updates the directory where main.log should be looked up.
func (h *Handler) SetLogDirectory(dir string) {
	if dir == "" {
		return
	}
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	h.logDir = dir
}

// SetPostAuthHook registers a hook to be called after auth record creation but before persistence.
func (h *Handler) SetPostAuthHook(hook coreauth.PostAuthHook) {
	h.postAuthHook = hook
}

// SetPostAuthPersistHook registers a hook to be called after auth persistence.
func (h *Handler) SetPostAuthPersistHook(hook coreauth.PostAuthHook) {
	h.postAuthPersistHook = hook
}

// Middleware enforces session authentication for management endpoints.
func (h *Handler) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-CPA-VERSION", buildinfo.Version)
		c.Header("X-CPA-COMMIT", buildinfo.Commit)
		c.Header("X-CPA-BUILD-DATE", buildinfo.BuildDate)
		c.Header("X-CPA-SUPPORT-PLUGIN", pluginhost.SupportPluginHeaderValue())

		if _, ok := h.authenticateSession(c, true); !ok {
			return
		}
		c.Next()
	}
}

// persist saves the current in-memory config to disk.
func (h *Handler) persist(c *gin.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.persistLocked(c)
}

// persistLocked saves the current in-memory config to disk.
// It expects the caller to hold h.mu.
func (h *Handler) persistLocked(c *gin.Context) bool {
	// Preserve comments when writing
	if err := config.SaveConfigPreserveComments(h.configFilePath, h.cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save config: %v", err)})
		return false
	}
	snapshot := h.reloadSnapshotConfigLocked()
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
	var reqCtx context.Context
	if c != nil && c.Request != nil {
		reqCtx = c.Request.Context()
	}
	h.reloadConfigAfterManagementSaveAsync(reqCtx, snapshot)
	return true
}

// Helper methods for simple types
func (h *Handler) updateBoolField(c *gin.Context, set func(bool)) {
	var body struct {
		Value *bool `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}

func (h *Handler) updateIntField(c *gin.Context, set func(int)) {
	var body struct {
		Value *int `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}

func (h *Handler) updateStringField(c *gin.Context, set func(string)) {
	var body struct {
		Value *string `json:"value"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	set(*body.Value)
	h.persist(c)
}

// Package main provides the entry point for the CLI Proxy API server.
// This server acts as a proxy that provides OpenAI/Gemini/Claude compatible API interfaces
// for CLI models, allowing CLI models to be used with tools and libraries designed for standard AI APIs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joho/godotenv"
	configaccess "github.com/router-for-me/CLIProxyAPI/v7/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/cmd"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/misc"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/safemode"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/store"
	_ "github.com/router-for-me/CLIProxyAPI/v7/internal/translator"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestats"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/util"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v7/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

var (
	Version           = "dev"
	Commit            = "none"
	BuildDate         = "unknown"
	DefaultConfigPath = ""
)

// init initializes the shared logger setup.
func init() {
	logging.SetupBaseLogger()
	buildinfo.Version = Version
	buildinfo.Commit = Commit
	buildinfo.BuildDate = BuildDate
}

func shouldEnableExampleAPIKeySafeMode(cfg *config.Config, commandMode, cloudConfigMissing, homeMode bool) bool {
	if cfg == nil || commandMode || homeMode || cloudConfigMissing {
		return false
	}
	return safemode.HasExampleAPIKeys(cfg.APIKeys)
}

func requiredPostgresConfig(lookup func(...string) (string, bool)) (dsn, schema string, err error) {
	if lookup == nil {
		return "", "", fmt.Errorf("PGSTORE_DSN is required")
	}
	dsn, ok := lookup("PGSTORE_DSN", "pgstore_dsn")
	if !ok {
		return "", "", fmt.Errorf("PGSTORE_DSN is required; local, Git, and object storage backends are disabled")
	}
	schema, _ = lookup("PGSTORE_SCHEMA", "pgstore_schema")
	return dsn, schema, nil
}

// main is the entry point of the application.
// It parses command-line flags, loads configuration, and starts the appropriate
// service based on the provided flags (login, codex-login, or server mode).
func main() {
	fmt.Printf("CLIProxyAPI Version: %s, Commit: %s, BuiltAt: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	// Command-line flags to control the application's behavior.
	var codexLogin bool
	var codexDeviceLogin bool
	var claudeLogin bool
	var noBrowser bool
	var oauthCallbackPort int
	var antigravityLogin bool
	var kimiLogin bool
	var xaiLogin bool
	var vertexImport string
	var vertexImportPrefix string
	var configPath string
	var homeJWT string
	var homeDisableClusterDiscovery bool
	var localModel bool

	// Define command-line flags for different operation modes.
	flag.BoolVar(&codexLogin, "codex-login", false, "Login to Codex using OAuth")
	flag.BoolVar(&codexDeviceLogin, "codex-device-login", false, "Login to Codex using device code flow")
	flag.BoolVar(&claudeLogin, "claude-login", false, "Login to Claude using OAuth")
	flag.BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically for OAuth")
	flag.IntVar(&oauthCallbackPort, "oauth-callback-port", 0, "Override OAuth callback port (defaults to provider-specific port)")
	flag.BoolVar(&antigravityLogin, "antigravity-login", false, "Login to Antigravity using OAuth")
	flag.BoolVar(&kimiLogin, "kimi-login", false, "Login to Kimi using OAuth")
	flag.BoolVar(&xaiLogin, "xai-login", false, "Login to xAI using OAuth")
	flag.StringVar(&configPath, "config", DefaultConfigPath, "Configure File Path")
	flag.StringVar(&vertexImport, "vertex-import", "", "Import Vertex service account key JSON file")
	flag.StringVar(&vertexImportPrefix, "vertex-import-prefix", "", "Prefix for Vertex model namespacing (use with -vertex-import)")
	flag.StringVar(&homeJWT, "home-jwt", "", "Home control plane JWT for mTLS certificate bootstrap and connection")
	flag.BoolVar(&homeDisableClusterDiscovery, "home-disable-cluster-discovery", false, "Disable Home CLUSTER NODES discovery and keep using the configured -home-jwt address")
	flag.BoolVar(&localModel, "local-model", false, "Use embedded models.json and codex_client_models.json only, skip remote model catalog fetching")

	flag.CommandLine.Usage = func() {
		out := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(out, "Usage of %s\n", os.Args[0])
		flag.CommandLine.VisitAll(func(f *flag.Flag) {
			s := fmt.Sprintf("  -%s", f.Name)
			name, unquoteUsage := flag.UnquoteUsage(f)
			if name != "" {
				s += " " + name
			}
			if len(s) <= 4 {
				s += "	"
			} else {
				s += "\n    "
			}
			if unquoteUsage != "" {
				s += unquoteUsage
			}
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
				s += fmt.Sprintf(" (default %s)", f.DefValue)
			}
			_, _ = fmt.Fprint(out, s+"\n")
		})
	}

	pluginHost := pluginhost.New()
	if bootstrapCfg := loadPluginBootstrapConfig(pluginBootstrapConfigPath(os.Args[1:], DefaultConfigPath)); bootstrapCfg != nil {
		pluginHost.ApplyConfig(context.Background(), bootstrapCfg)
		pluginHost.RegisterCommandLineFlags(context.Background(), flag.CommandLine)
	}

	// Parse the command-line flags.
	flag.Parse()

	// Core application variables.
	var err error
	var cfg *config.Config
	var isCloudDeploy bool
	var (
		pgStoreDSN    string
		pgStoreSchema string
		pgStoreInst   *store.PostgresStore
	)

	wd, err := os.Getwd()
	if err != nil {
		log.Errorf("failed to get working directory: %v", err)
		return
	}

	// Load environment variables from .env if present.
	if errLoad := godotenv.Load(filepath.Join(wd, ".env")); errLoad != nil {
		if !errors.Is(errLoad, os.ErrNotExist) {
			log.WithError(errLoad).Warn("failed to load .env file")
		}
	}

	lookupEnv := func(keys ...string) (string, bool) {
		for _, key := range keys {
			if value, ok := os.LookupEnv(key); ok {
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					return trimmed, true
				}
			}
		}
		return "", false
	}
	if strings.TrimSpace(homeJWT) == "" {
		if v, ok := lookupEnv("HOME_JWT", "home_jwt"); ok {
			homeJWT = v
		}
	}
	if strings.TrimSpace(homeJWT) != "" {
		log.Error("Home control plane mode is unavailable because PostgreSQL is the required persistence backend")
		return
	}
	pgStoreDSN, pgStoreSchema, err = requiredPostgresConfig(lookupEnv)
	if err != nil {
		log.Error(err)
		return
	}

	// Check for cloud deploy mode only on first execution
	// Read env var name in uppercase: DEPLOY
	deployEnv := os.Getenv("DEPLOY")
	if deployEnv == "cloud" {
		isCloudDeploy = true
	}

	// PostgreSQL is the only persistent source. The local workspace is temporary and removed on exit.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	pgStoreInst, err = store.NewPostgresStore(ctx, store.PostgresStoreConfig{
		DSN:    pgStoreDSN,
		Schema: pgStoreSchema,
	})
	cancel()
	if err != nil {
		log.Errorf("failed to initialize required postgres store: %v", err)
		return
	}
	defer func() {
		if errClose := pgStoreInst.Close(); errClose != nil {
			log.WithError(errClose).Error("failed to close postgres store")
		}
	}()
	examplePath := filepath.Join(wd, "config.example.yaml")
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	errBootstrap := pgStoreInst.Bootstrap(ctx, examplePath)
	cancel()
	if errBootstrap != nil {
		log.Errorf("failed to bootstrap postgres-backed data: %v", errBootstrap)
		return
	}
	configFilePath := pgStoreInst.ConfigPath()
	cfg, err = config.LoadConfigOptional(configFilePath, isCloudDeploy)
	if err != nil {
		log.Errorf("failed to load config: %v", err)
		return
	}
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Home.Enabled {
		log.Error("Home control plane configuration is unavailable because PostgreSQL is the required persistence backend")
		return
	}
	cfg.AuthDir = pgStoreInst.AuthDir()
	log.Infof("required postgres store enabled; temporary workspace: %s", pgStoreInst.WorkDir())

	// In cloud deploy mode, check if we have a valid configuration
	var configFileExists bool
	if isCloudDeploy {
		if info, errStat := os.Stat(configFilePath); errStat != nil {
			// Don't mislead: API server will not start until configuration is provided.
			log.Info("Cloud deploy mode: No configuration file detected; standing by for configuration")
			configFileExists = false
		} else if info.IsDir() {
			log.Info("Cloud deploy mode: Config path is a directory; standing by for configuration")
			configFileExists = false
		} else if cfg.Port == 0 {
			// LoadConfigOptional returns empty config when file is empty or invalid.
			// Config file exists but is empty or invalid; treat as missing config
			log.Info("Cloud deploy mode: Configuration file is empty or invalid; standing by for valid configuration")
			configFileExists = false
		} else {
			log.Info("Cloud deploy mode: Configuration file detected; starting service")
			configFileExists = true
		}
	}
	redisqueue.SetUsageStatisticsEnabled(cfg.UsageStatisticsEnabled)
	redisqueue.SetRetentionSeconds(cfg.RedisUsageQueueRetentionSeconds)
	coreauth.SetQuotaCooldownDisabled(cfg.DisableCooling)
	coreauth.SetTransientErrorCooldownSeconds(cfg.TransientErrorCooldownSeconds)

	if err = logging.ConfigureLogOutput(cfg); err != nil {
		log.Errorf("failed to configure log output: %v", err)
		return
	}

	log.Infof("CLIProxyAPI Version: %s, Commit: %s, BuiltAt: %s", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	// Set the log level based on the configuration.
	util.SetLogLevel(cfg)

	if resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir); errResolveAuthDir != nil {
		log.Errorf("failed to resolve auth directory: %v", errResolveAuthDir)
		return
	} else {
		cfg.AuthDir = resolvedAuthDir
	}
	// Create login options to be used in authentication flows.
	options := &cmd.LoginOptions{
		NoBrowser:    noBrowser,
		CallbackPort: oauthCallbackPort,
	}

	commandMode := vertexImport != "" || antigravityLogin || codexLogin || codexDeviceLogin || claudeLogin || kimiLogin || xaiLogin
	cloudConfigMissing := isCloudDeploy && !configFileExists
	homeMode := cfg != nil && cfg.Home.Enabled
	exampleAPIKeySafeMode := shouldEnableExampleAPIKeySafeMode(cfg, commandMode, cloudConfigMissing, homeMode)
	serverOptions := []api.ServerOption(nil)
	if exampleAPIKeySafeMode {
		matches := safemode.ExampleAPIKeys(cfg.APIKeys)
		log.WithField("api_keys", strings.Join(matches, ",")).Error("unsafe example API key configured; proxy API endpoints disabled until api-keys is updated")
		serverOptions = append(serverOptions, api.WithExampleAPIKeySafeMode())
	}

	// Register the required PostgreSQL token store for all authentication flows.
	sdkAuth.RegisterTokenStore(pgStoreInst)

	// Register built-in access providers before constructing services.
	configaccess.Register(&cfg.SDKConfig)
	pluginHost.ApplyConfig(context.Background(), cfg)
	if pluginHost.HasTriggeredCommandLineFlags() {
		if exitCode, handled := pluginHost.ExecuteCommandLine(context.Background(), os.Args[0], os.Args[1:], configFilePath, flag.CommandLine); handled {
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return
		}
	}

	// Handle different command modes based on the provided flags.

	if vertexImport != "" {
		// Handle Vertex service account import
		cmd.DoVertexImport(cfg, vertexImport, vertexImportPrefix)
	} else if antigravityLogin {
		// Handle Antigravity login
		cmd.DoAntigravityLogin(cfg, options)
	} else if codexLogin {
		// Handle Codex login
		cmd.DoCodexLogin(cfg, options)
	} else if codexDeviceLogin {
		// Handle Codex device-code login
		cmd.DoCodexDeviceLogin(cfg, options)
	} else if claudeLogin {
		// Handle Claude login
		cmd.DoClaudeLogin(cfg, options)
	} else if kimiLogin {
		cmd.DoKimiLogin(cfg, options)
	} else if xaiLogin {
		cmd.DoXAILogin(cfg, options)
	} else {
		// In cloud deploy mode without config file, just wait for shutdown signals
		if isCloudDeploy && !configFileExists {
			// No config file available, just wait for shutdown
			cmd.WaitForCloudDeploy()
			return
		}
		if localModel {
			log.Info("Local model mode: using embedded model catalogs, remote model updates disabled")
		}
		ctxUsageStats, cancelUsageStats := context.WithTimeout(context.Background(), 30*time.Second)
		usageStatsStore, errUsageStats := usagestats.ConfigurePostgres(ctxUsageStats, pgStoreDSN, pgStoreSchema, cfg.UsageStatisticsEnabled)
		cancelUsageStats()
		if errUsageStats != nil {
			log.Errorf("failed to initialize required postgres usage statistics store: %v", errUsageStats)
			return
		}
		defer usagestats.CloseIf(usageStatsStore)
		legacyUsagePaths := []string{
			filepath.Join(wd, "usage-statistics.json"),
			filepath.Join(filepath.Dir(configPath), "usage-statistics.json"),
		}
		legacyStoreBase := util.WritablePath()
		if value, okLegacyPath := lookupEnv("PGSTORE_LOCAL_PATH", "pgstore_local_path"); okLegacyPath {
			legacyStoreBase = value
		}
		if legacyStoreBase == "" {
			legacyStoreBase = wd
		}
		legacyUsagePaths = append(legacyUsagePaths, filepath.Join(legacyStoreBase, "pgstore", "config", "usage-statistics.json"))
		importLegacyUsageStatistics(usageStatsStore, legacyUsagePaths...)
		serverOptions = append(serverOptions, api.WithUsageStatsStore(usageStatsStore))
		misc.StartAntigravityVersionUpdater(context.Background())
		startModelCatalogUpdaters(localModel, cfg.Home.Enabled)
		cmd.StartServiceWithPluginHost(cfg, configFilePath, pluginHost, serverOptions...)
	}
}

func importLegacyUsageStatistics(store *usagestats.Store, paths ...string) {
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = filepath.Clean(strings.TrimSpace(path))
		if path == "." || path == "" {
			continue
		}
		if _, exists := seen[path]; exists {
			continue
		}
		seen[path] = struct{}{}
		data, errRead := os.ReadFile(path)
		if errors.Is(errRead, os.ErrNotExist) {
			continue
		}
		if errRead != nil {
			log.WithError(errRead).Warnf("failed to read legacy usage statistics from %s", path)
			continue
		}
		imported, errImport := store.ImportSnapshotIfEmpty(data)
		if errImport != nil {
			log.WithError(errImport).Warnf("failed to import legacy usage statistics from %s", path)
			continue
		}
		if imported {
			log.Infof("legacy usage statistics imported into PostgreSQL from %s", path)
		}
		return
	}
}

// modelCatalogUpdaterPlan decides which remote model catalogs should refresh.
// Codex client templates still refresh under Home mode because the model list
// comes from Home IDs while template metadata stays edge-local.
func modelCatalogUpdaterPlan(localModel, homeEnabled bool) (startModels, startCodexClient bool) {
	if localModel {
		return false, false
	}
	return !homeEnabled, true
}

func startModelCatalogUpdaters(localModel, homeEnabled bool) {
	startModels, startCodexClient := modelCatalogUpdaterPlan(localModel, homeEnabled)
	if startCodexClient {
		registry.StartCodexClientModelsUpdater(context.Background())
	}
	if startModels {
		registry.StartModelsUpdater(context.Background())
	} else if homeEnabled {
		log.Info("Home mode: remote models.json updates disabled; Codex client model list follows Home model IDs")
	}
}

func pluginBootstrapConfigPath(args []string, defaultPath string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return defaultPluginBootstrapConfigPath(defaultPath)
		case arg == "-config" || arg == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
			return defaultPluginBootstrapConfigPath(defaultPath)
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config=")
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return defaultPluginBootstrapConfigPath(defaultPath)
}

func defaultPluginBootstrapConfigPath(defaultPath string) string {
	if strings.TrimSpace(defaultPath) != "" {
		return defaultPath
	}
	wd, errGetwd := os.Getwd()
	if errGetwd != nil {
		return "config.yaml"
	}
	return filepath.Join(wd, "config.yaml")
}

func loadPluginBootstrapConfig(path string) *config.Config {
	raw, errReadFile := os.ReadFile(path)
	if errReadFile != nil {
		if !errors.Is(errReadFile, os.ErrNotExist) {
			log.Warnf("failed to read plugin bootstrap config: %v", errReadFile)
		}
		cfg := &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		cfg := &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	cfg, errParseConfig := config.ParseConfigBytes(raw)
	if errParseConfig != nil {
		log.Warnf("failed to parse plugin bootstrap config: %v", errParseConfig)
		cfg = &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	return cfg
}

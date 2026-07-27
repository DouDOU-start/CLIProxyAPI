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
	"gopkg.in/yaml.v3"
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

type postgresBootstrapConfig struct {
	PostgreSQL struct {
		DSN    string `yaml:"dsn"`
		Schema string `yaml:"schema"`
	} `yaml:"postgresql"`
}

func loadRequiredPostgresConfig(path string) (dsn, schema string, err error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", "", fmt.Errorf("database bootstrap config path is required")
	}
	data, errRead := os.ReadFile(path)
	if errRead != nil {
		if errors.Is(errRead, os.ErrNotExist) {
			return "", "", fmt.Errorf("database bootstrap config %s is required", path)
		}
		return "", "", fmt.Errorf("read database bootstrap config %s: %w", path, errRead)
	}
	var bootstrap postgresBootstrapConfig
	if errUnmarshal := yaml.Unmarshal(data, &bootstrap); errUnmarshal != nil {
		return "", "", fmt.Errorf("parse database bootstrap config %s: %w", path, errUnmarshal)
	}
	dsn = strings.TrimSpace(bootstrap.PostgreSQL.DSN)
	schema = strings.TrimSpace(bootstrap.PostgreSQL.Schema)
	if dsn == "" {
		return "", "", fmt.Errorf("postgresql.dsn is required in %s", path)
	}
	return dsn, schema, nil
}

func databaseBootstrapConfigPath(args []string, defaultPath string) (string, error) {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			return defaultPath, nil
		case arg == "-config" || arg == "--config":
			if index+1 >= len(args) {
				return "", fmt.Errorf("%s requires a path", arg)
			}
			return args[index+1], nil
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config="), nil
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config="), nil
		}
	}
	return defaultPath, nil
}

func resolveDatabaseBootstrapConfigPath(path, workingDirectory string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return filepath.Join(workingDirectory, "config.yaml")
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workingDirectory, path)
}

func commandLineRequestsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "-help" || arg == "--help" {
			return true
		}
	}
	return false
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
	flag.StringVar(&configPath, "config", DefaultConfigPath, "Database bootstrap YAML path")
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

	pluginHost := pluginhost.New()
	if commandLineRequestsHelp(os.Args[1:]) {
		flag.Parse()
		return
	}

	bootstrapPath, errBootstrapPath := databaseBootstrapConfigPath(os.Args[1:], DefaultConfigPath)
	if errBootstrapPath != nil {
		log.Error(errBootstrapPath)
		return
	}
	configPath = resolveDatabaseBootstrapConfigPath(bootstrapPath, wd)

	pgStoreDSN, pgStoreSchema, err = loadRequiredPostgresConfig(configPath)
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
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	errBootstrap := pgStoreInst.Bootstrap(ctx)
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

	pluginHost.ApplyConfig(context.Background(), cfg)
	pluginHost.RegisterCommandLineFlags(context.Background(), flag.CommandLine)

	// Parse the command-line flags after database-backed plugin settings are available.
	loadedBootstrapPath := configPath
	flag.Parse()
	parsedConfigPath := resolveDatabaseBootstrapConfigPath(configPath, wd)
	if parsedConfigPath != loadedBootstrapPath {
		log.Errorf("database bootstrap config path changed during flag parsing: %s", parsedConfigPath)
		return
	}
	configPath = parsedConfigPath
	if strings.TrimSpace(homeJWT) != "" {
		log.Error("Home control plane mode is unavailable because PostgreSQL is the required persistence backend")
		return
	}

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
	redisqueue.SetUsageStatisticsEnabled(true)
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
	serverOptions := []api.ServerOption{api.WithConfigPersistHook(pgStoreInst.PersistConfig)}
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
		usageStatsStore, errUsageStats := usagestats.ConfigurePostgres(ctxUsageStats, pgStoreDSN, pgStoreSchema, true)
		cancelUsageStats()
		if errUsageStats != nil {
			log.Errorf("failed to initialize required postgres usage statistics store: %v", errUsageStats)
			return
		}
		defer usagestats.CloseIf(usageStatsStore)
		serverOptions = append(serverOptions, api.WithUsageStatsStore(usageStatsStore))
		misc.StartAntigravityVersionUpdater(context.Background())
		startModelCatalogUpdaters(localModel, cfg.Home.Enabled)
		cmd.StartServiceWithPluginHost(cfg, configFilePath, pluginHost, serverOptions...)
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

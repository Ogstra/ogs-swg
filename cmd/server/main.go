package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ogstra/ogs-swg/internal/api"
	"github.com/Ogstra/ogs-swg/internal/core"
	"github.com/Ogstra/ogs-swg/internal/sys"
)

func main() {
	samplerOnly := flag.Bool("sampler-only", false, "Run sampler only (no HTTP server)")
	configPath := flag.String("config", "config.json", "Path to panel config.json")
	singboxConfigPath := flag.String("singbox-config", "", "Override sing-box config path (optional)")
	logPath := flag.String("log", "", "Path to access.log")
	dbPath := flag.String("db", "", "Path to stats.db")
	wgTestMode := flag.Bool("wg-test-mode", false, "Enable simulated WireGuard mode (no wg/systemd dependency)")
	flag.Parse()

	cfg := core.LoadConfig(*configPath)
	if *singboxConfigPath != "" {
		cfg.SingboxConfigPath = *singboxConfigPath
	}
	if *logPath != "" {
		cfg.AccessLogPath = *logPath
	}
	if *dbPath != "" {
		cfg.DatabasePath = *dbPath
	}
	if *wgTestMode {
		cfg.WireGuardTestMode = true
	}
	cfg.ApplyWireGuardTestModeDefaults()

	log.Printf("Starting OGS XWG...")
	log.Printf(
		"Config loaded: listen=%s db=%s singbox=%t wireguard=%t execution_mode=%s",
		cfg.ListenAddr,
		cfg.DatabasePath,
		cfg.EnableSingbox,
		cfg.EnableWireGuard,
		cfg.ExecutionMode,
	)

	if *samplerOnly {
		log.Printf("Sampler-only mode: starting stats sampler without HTTP server")
		store, err := core.NewStore(cfg.DatabasePath)
		if err != nil {
			log.Fatalf("Failed to open store: %v", err)
		}
		defer store.Close()

		var executor core.SystemExecutor
		if cfg.ExecutionMode == "docker_local" {
			log.Printf("Initializing Docker Local Executor (host D-Bus mode)")
			executor = sys.NewDockerLocalExecutor(cfg)
		} else {
			if cfg.WireGuardTestMode {
				log.Printf("Initializing Local Executor (WireGuard test mode enabled, dir=%s)", cfg.WireGuardConfigDir)
			} else {
				log.Printf("Initializing Local Executor")
			}
			executor = sys.NewLocalExecutor(
				sys.WithWireGuardConfigDir(cfg.WireGuardConfigDir),
				sys.WithWireGuardTestMode(cfg.WireGuardTestMode),
			)
		}

		sbClient := core.NewSingboxClient(cfg.SingboxAPIAddr, executor)
		sampler := core.NewStatsSampler(sbClient, store, cfg)
		sampler.Start()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
		<-quit
		log.Println("Stopping sampler...")
		sampler.Stop()
		sbClient.Close()
		return
	}

	var server *api.Server
	go func() {
		server = api.StartServer(cfg)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
	if server != nil {
		server.Stop()
	}
}

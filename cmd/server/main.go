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

	log.Printf("Starting OGS XWG...")
	log.Printf("Config: %+v", cfg)

	if *samplerOnly {
		log.Printf("Sampler-only mode: starting stats sampler without HTTP server")
		store, err := core.NewStore(cfg.DatabasePath)
		if err != nil {
			log.Fatalf("Failed to open store: %v", err)
		}
		defer store.Close()

		defer store.Close()

		var executor core.SystemExecutor
		if cfg.SSHHost != "" {
			log.Printf("Initializing SSH Executor for host: %s", cfg.SSHHost)
			executor = sys.NewSSHExecutor(cfg)
		} else {
			log.Printf("Initializing Local Executor")
			executor = sys.NewLocalExecutor()
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

package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"github.com/Ogstra/ogs-swg/api"
	"github.com/Ogstra/ogs-swg/core"
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

		sbClient := core.NewSingboxClient(cfg.SingboxAPIAddr)
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

	go func() {
		api.StartServer(cfg)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down...")
}

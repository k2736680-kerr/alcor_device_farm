package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Ad-Quanta/alcor-device-farm/internal/buildinfo"
	"github.com/Ad-Quanta/alcor-device-farm/internal/config"
	"github.com/Ad-Quanta/alcor-device-farm/internal/logging"
	"github.com/Ad-Quanta/alcor-device-farm/internal/server"
)

func main() {
	version := flag.Bool("version", false, "print version information and exit")
	configPath := flag.String("config", "", "path to YAML configuration file")
	checkConfig := flag.Bool("check-config", false, "validate configuration and exit")
	flag.Parse()

	if *version {
		fmt.Fprintln(os.Stdout, buildinfo.String("device-farm-server"))
		return
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
		os.Exit(1)
	}
	if *checkConfig {
		fmt.Fprintln(os.Stdout, "configuration is valid")
		return
	}

	logger, err := logging.New(cfg.Log, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logging error: %v\n", err)
		os.Exit(1)
	}
	logger.Info("configuration loaded", "config", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := server.Run(ctx, cfg, logger); err != nil {
		logger.Error("device farm server stopped with error", "error", err)
		os.Exit(1)
	}
}

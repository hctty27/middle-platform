package main

import (
	"flag"
	"log/slog"
	"os"

	"hitall.com/middle-platform/config-center-access/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "YAML 配置文件")
	flag.Parse()

	if err := app.Run(*configPath); err != nil {
		slog.Error("配置接入服务已停止", "error", err)
		os.Exit(1)
	}
}

package main

import (
	"flag"
	"log/slog"
	"os"

	"hitall.com/middle-platform/sdk-service/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "YAML 配置文件")
	flag.Parse()

	if err := app.Run(*configPath); err != nil {
		slog.Error("SDK Service 启动或运行失败", "error", err)
		os.Exit(1)
	}
}

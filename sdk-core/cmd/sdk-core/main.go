package main

import (
	"flag"
	"log/slog"
	"os"

	"hitall.com/middle-platform/sdk-core/internal/app"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "YAML 配置文件")
	flag.Parse()

	if err := app.Run(*configPath); err != nil {
		slog.Error("SDK Core 启动或运行失败", "error", err)
		os.Exit(1)
	}
}

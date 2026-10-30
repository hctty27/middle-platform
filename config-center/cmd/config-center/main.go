package main

import (
	"log/slog"
    "flag"
	"os"
	"github.com/hctty27/middle-platform/config-center/internal/app"
)

// main 仅负责启动进程，服务生命周期交由 app 管理。
func main() {
    configPath := flag.String("config", "", "YAML configuration file")
    flag.Parse()
    if err := app.Run(*configPath); err != nil {
        slog.Error("config-center stopped", "error", err)
        os.Exit(1)
    }
}

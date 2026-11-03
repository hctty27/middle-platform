// 配置中心服务入口：进程生命周期交由 app 负责。
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
        slog.Error("配置中心启动或运行失败", "error", err)
        os.Exit(1)
    }
}

// 配置中心服务进程入口：不承载业务逻辑，初始化和生命周期统一由 app.Run 负责。
package main

import (
	"log/slog"
	"os"
	"github.com/hctty27/middle-platform/config-center/internal/app"
)

// main 仅负责启动进程，服务生命周期交由 app 管理。
func main() {
    if err := app.Run(); err != nil {
        slog.Error("config-center stopped", "error", err)
        os.Exit(1)
    }
}

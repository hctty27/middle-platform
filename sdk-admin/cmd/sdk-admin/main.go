package main

import (
 "flag"
 "log/slog"
 "os"

 "hitall.com/middle-platform/sdk-admin/internal/app"
)

func main() {
 configPath := flag.String("config", "configs/config.yaml", "YAML configuration file")
 flag.Parse()
 if err := app.Run(*configPath); err != nil {
  slog.Error("SDK Admin 启动或运行失败","error",err)
  os.Exit(1)
 }
}

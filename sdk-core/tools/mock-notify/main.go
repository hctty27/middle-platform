// 本地联调专用：模拟游戏服接收 SDK Core 的发货通知，不验签、不发货。
package main

import (
	"encoding/hex"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
	"encoding/json"
)

type responseMode string

const (
	modeSuccess responseMode = "success"
	modeFail    responseMode = "fail"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9000", "模拟服务监听地址")
	mode := flag.String("mode", string(modeSuccess), "响应模式：success 或 fail")
	flag.Parse()

	selected := responseMode(*mode)
	if selected != modeSuccess && selected != modeFail {
		log.Fatalf("无效模式 %q：只能选择 success 或 fail", *mode)
	}

	logger := log.New(os.Stdout, "[mock-notify] ", log.LstdFlags)
	server := &http.Server{
		Addr:              *addr,
		Handler:           newHandler(selected, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
	}

	logger.Printf("模拟游戏通知服务已启动：addr=%s mode=%s path=/pay/notify", *addr, selected)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newHandler(mode responseMode, logger *log.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"status\":\"UP\"}")
	})

	mux.HandleFunc("POST /pay/notify", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
		if err != nil || !json.Valid(body) {
			http.Error(w, "请求体 JSON 格式不合法", http.StatusBadRequest)
			return
		}

		nonce := r.Header.Get("X-SDK-Nonce")
		rawNonce, err := hex.DecodeString(nonce)
		if err != nil || len(rawNonce) != 8 || strings.ToLower(nonce) != nonce {
			http.Error(w, "X-SDK-Nonce 格式不合法", http.StatusBadRequest)
			return
		}

		logger.Printf("收到通知 appid=%s timestamp=%s nonce=%s body=%s",
			r.Header.Get("X-SDK-AppID"), r.Header.Get("X-SDK-Timestamp"), nonce, body)

		if mode == modeFail {
			logger.Printf("模拟失败：返回 HTTP 500，触发 SDK Core 补偿重试")
			http.Error(w, "模拟通知失败", http.StatusInternalServerError)
			return
		}

		// SDK Core 只接受 HTTP 200 且 Body 与 X-SDK-Nonce 完全一致的确认响应。
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, nonce)
		logger.Printf("通知确认成功 nonce=%s", nonce)
	})
	return mux
}

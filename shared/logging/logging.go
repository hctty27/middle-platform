// Package logging 提供所有 Go 服务共用的结构化日志及 HTTP 请求日志。
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

const RequestIDHeader = "X-Request-ID"

type Config struct {
	Level         string `yaml:"level"`
	Format        string `yaml:"format"`
	Output        string `yaml:"output"`
	AccessEnabled bool   `yaml:"accessEnabled"`
	SlowThreshold string `yaml:"slowThreshold"`
}

func Default() Config {
	return Config{
		Level: "info", Format: "json", Output: "stdout",
		AccessEnabled: true, SlowThreshold: "500ms",
	}
}

// ApplyEnv 保证 LOG_* 环境变量可以覆盖 YAML，并拒绝错误的布尔值。
func (c *Config) ApplyEnv() error {
	for _, item := range []struct{ key string; dest *string }{
		{"LOG_LEVEL", &c.Level},
		{"LOG_FORMAT", &c.Format},
		{"LOG_OUTPUT", &c.Output},
		{"LOG_SLOW_THRESHOLD", &c.SlowThreshold},
	} {
		if value, ok := os.LookupEnv(item.key); ok {
			*item.dest = value
		}
	}
	if value, ok := os.LookupEnv("LOG_ACCESS_ENABLED"); ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid LOG_ACCESS_ENABLED: %w", err)
		}
		c.AccessEnabled = parsed
	}
	return c.Validate()
}

func (c Config) Validate() error {
	switch strings.ToLower(c.Level) {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("invalid log.level %q", c.Level)
	}
	switch strings.ToLower(c.Format) {
	case "json", "text":
	default:
		return fmt.Errorf("invalid log.format %q", c.Format)
	}
	switch strings.ToLower(c.Output) {
	case "stdout", "stderr":
	default:
		return fmt.Errorf("invalid log.output %q", c.Output)
	}
	threshold, err := time.ParseDuration(c.SlowThreshold)
	if err != nil || threshold <= 0 {
		return fmt.Errorf("invalid log.slowThreshold %q", c.SlowThreshold)
	}
	return nil
}

// Init 仅在进程入口调用一次，运行日志由 stdout/stderr 交给部署平台采集和轮转。
func Init(service string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	levels := map[string]slog.Level{
		"debug": slog.LevelDebug, "info": slog.LevelInfo,
		"warn": slog.LevelWarn, "error": slog.LevelError,
	}
	output := os.Stdout
	if strings.EqualFold(c.Output, "stderr") {
		output = os.Stderr
	}
	opts := &slog.HandlerOptions{Level: levels[strings.ToLower(c.Level)]}
	var handler slog.Handler
	if strings.EqualFold(c.Format, "text") {
		handler = slog.NewTextHandler(output, opts)
	} else {
		handler = slog.NewJSONHandler(output, opts)
	}
	instance := os.Getenv("INSTANCE_ID")
	if instance == "" {
		instance, _ = os.Hostname()
	}
	slog.SetDefault(slog.New(handler).With("service", service, "instance", instance))
	return nil
}

type requestIDKey struct{}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// FromContext 为业务日志自动补充本次请求的关联 ID。
func FromContext(ctx context.Context) *slog.Logger {
	if id := RequestID(ctx); id != "" {
		return slog.Default().With("request_id", id)
	}
	return slog.Default()
}

var fallbackCounter atomic.Uint64

func newRequestID() string {
	var random [12]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	return fmt.Sprintf("%x-%x", time.Now().UnixNano(), fallbackCounter.Add(1))
}

func validRequestID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, ch := range value {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

// Unwrap 让 http.ResponseController 可以访问底层 ResponseWriter 的可选能力。
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// HTTP 统一生成或校验 Request ID，记录状态码和耗时，捕获 panic；不会记录 URL 查询串、请求体或密钥。
func HTTP(c Config, next http.Handler) http.Handler {
	threshold, _ := time.ParseDuration(c.SlowThreshold)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(RequestIDHeader)
		if !validRequestID(id) {
			id = newRequestID()
		}
		ctx := context.WithValue(r.Context(), requestIDKey{}, id)
		w.Header().Set(RequestIDHeader, id)
		writer := &statusWriter{ResponseWriter: w}
		start := time.Now()

		defer func() {
			if recovered := recover(); recovered != nil {
				if writer.status == 0 {
					http.Error(writer, "internal server error", http.StatusInternalServerError)
				}
				FromContext(ctx).ErrorContext(ctx, "http handler panic", "panic_type", fmt.Sprintf("%T", recovered))
			}
			if writer.status == 0 {
				writer.status = http.StatusOK
			}
			elapsed := time.Since(start)
			slow := elapsed >= threshold
			if !c.AccessEnabled && writer.status < 400 && !slow {
				return
			}
			if (r.URL.Path == "/health/live" || r.URL.Path == "/health/ready") && writer.status < 400 && !slow {
				return
			}
			level := slog.LevelInfo
			if writer.status >= 500 {
				level = slog.LevelError
			} else if writer.status >= 400 || slow {
				level = slog.LevelWarn
			}
			path := r.Pattern
			if path == "" {
				path = r.URL.EscapedPath()
			}
			slog.Log(ctx, level, "http request",
				"request_id", id, "method", r.Method, "path", path,
				"status", writer.status, "duration_ms", elapsed.Milliseconds())
		}()
		next.ServeHTTP(writer, r.WithContext(ctx))
	})
}

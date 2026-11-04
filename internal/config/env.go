package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
)

var (
	ErrMissing = errors.New("config: required variable is not set")
	ErrInvalid = errors.New("config: invalid value")
)

type EnvLoader struct {
	lookup func(string) (string, bool)
}

var _ Loader = (*EnvLoader)(nil)

// NewEnvLoader allows tests to supply an environment without changing the process.
// A nil lookup uses os.LookupEnv.
func NewEnvLoader(lookup func(string) (string, bool)) *EnvLoader {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	return &EnvLoader{lookup: lookup}
}

func (l *EnvLoader) Load(ctx context.Context) (Config, error) {
	if err := ctx.Err(); err != nil {
		return Config{}, err
	}

	cfg := Config{
		HTTPAddr:  l.value("LF_HTTP_ADDR", ":8080"),
		AdminAddr: l.value("LF_ADMIN_ADDR", "127.0.0.1:6060"),
		TCPAddr:   l.value("LF_TCP_ADDR", ":7070"),
		PGDSN:     l.value("LF_PG_DSN", ""),
		MongoURI:  l.value("LF_MONGO_URI", ""),
		JWTSecret: l.value("LF_JWT_SECRET", ""),
	}
	var problems []error
	for _, addr := range []struct{ key, value string }{
		{"LF_HTTP_ADDR", cfg.HTTPAddr},
		{"LF_ADMIN_ADDR", cfg.AdminAddr},
		{"LF_TCP_ADDR", cfg.TCPAddr},
	} {
		if err := validateAddress(addr.value); err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", addr.key, err))
		}
	}
	for _, credential := range []struct{ key, value string }{
		{"LF_PG_DSN", cfg.PGDSN},
		{"LF_MONGO_URI", cfg.MongoURI},
		{"LF_JWT_SECRET", cfg.JWTSecret},
	} {
		if credential.value == "" {
			problems = append(problems, fmt.Errorf("%s: %w", credential.key, ErrMissing))
		}
	}
	if cfg.JWTSecret != "" && len(cfg.JWTSecret) < 32 {
		problems = append(problems, fmt.Errorf("LF_JWT_SECRET: %w: must be at least 32 bytes", ErrInvalid))
	}

	cacheSize, err := strconv.Atoi(l.value("LF_CACHE_SIZE", "100000"))
	if err != nil || cacheSize <= 0 {
		problems = append(problems, fmt.Errorf("LF_CACHE_SIZE: %w: must be an integer greater than zero", ErrInvalid))
	}
	cfg.CacheSize = cacheSize
	level, err := ParseLevel(l.value("LF_LOG_LEVEL", "info"))
	if err != nil {
		problems = append(problems, fmt.Errorf("LF_LOG_LEVEL: %w", err))
	}
	cfg.LogLevel = level

	if err := errors.Join(problems...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (l *EnvLoader) value(key, fallback string) string {
	value, ok := l.lookup(key)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func validateAddress(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err == nil {
		number, parseErr := strconv.Atoi(port)
		if parseErr == nil && number >= 1 && number <= 65535 {
			return nil
		}
	}
	// Parser errors can echo input; keep environment values out of diagnostics.
	return fmt.Errorf("%w: expected host:port with port in 1-65535", ErrInvalid)
}

func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("%w: expected debug, info, warn or error", ErrInvalid)
	}
}

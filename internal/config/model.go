// Package config defines how runtime configuration is obtained. Configuration
// is environment-only and validated at startup.
package config

import "log/slog"

// Config is the fully resolved runtime configuration.
type Config struct {
	HTTPAddr  string
	AdminAddr string
	TCPAddr   string
	PGDSN     string
	MongoURI  string
	JWTSecret string
	CacheSize int
	LogLevel  slog.Level
}

var _ slog.LogValuer = (*Config)(nil)

// LogValue keeps credentials out of structured logs, including for an empty Config.
func (c Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("http_addr", c.HTTPAddr),
		slog.String("admin_addr", c.AdminAddr),
		slog.String("tcp_addr", c.TCPAddr),
		slog.String("pg_dsn", "[redacted]"),
		slog.String("mongo_uri", "[redacted]"),
		slog.String("jwt_secret", "[redacted]"),
		slog.Int("cache_size", c.CacheSize),
		slog.Any("log_level", c.LogLevel),
	)
}

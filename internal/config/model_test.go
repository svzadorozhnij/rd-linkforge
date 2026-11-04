package config

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestConfigLogValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
	}{
		{name: "empty"},
		{
			name: "populated",
			cfg: Config{
				HTTPAddr: ":8081", AdminAddr: "localhost:6061", TCPAddr: ":7071",
				PGDSN: "private-test-dsn", MongoURI: "private-test-uri", JWTSecret: "private-test-secret",
				CacheSize: 123, LogLevel: slog.LevelWarn,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value := tc.cfg.LogValue()
			if value.Kind() != slog.KindGroup {
				t.Fatalf("kind = %v, want group", value.Kind())
			}
			want := map[string]slog.Value{
				"http_addr": slog.StringValue(tc.cfg.HTTPAddr), "admin_addr": slog.StringValue(tc.cfg.AdminAddr),
				"tcp_addr": slog.StringValue(tc.cfg.TCPAddr), "cache_size": slog.IntValue(tc.cfg.CacheSize),
				"log_level": slog.AnyValue(tc.cfg.LogLevel),
				"pg_dsn":    slog.StringValue("[redacted]"), "mongo_uri": slog.StringValue("[redacted]"),
				"jwt_secret": slog.StringValue("[redacted]"),
			}
			if len(value.Group()) != len(want) {
				t.Fatalf("got %d attributes, want %d", len(value.Group()), len(want))
			}
			for _, attr := range value.Group() {
				expected, ok := want[attr.Key]
				if !ok || !attr.Value.Equal(expected) {
					t.Errorf("unexpected log attribute %q", attr.Key)
				}
				delete(want, attr.Key)
			}
			if len(want) != 0 {
				t.Errorf("missing %d log attributes", len(want))
			}
		})
	}
}

func TestConfigLoggingHidesCredentials(t *testing.T) {
	values := validEnv(t)
	cfg, err := NewEnvLoader(envLookup(t, values)).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var output bytes.Buffer
			var handler slog.Handler
			if format == "text" {
				handler = slog.NewTextHandler(&output, nil)
			} else {
				handler = slog.NewJSONHandler(&output, nil)
			}
			slog.New(handler).Info("configuration", "config", cfg)
			for _, key := range []string{"LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET"} {
				if strings.Contains(output.String(), values[key]) {
					t.Errorf("log exposed %s", key)
				}
			}
			if got := strings.Count(output.String(), "[redacted]"); got != 3 {
				t.Errorf("got %d redactions, want 3", got)
			}
			if !strings.Contains(output.String(), ":8080") {
				t.Error("log must retain non-secret configuration fields")
			}
		})
	}
}

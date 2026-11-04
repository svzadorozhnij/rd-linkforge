package config

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"
	"time"
)

func validEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"LF_PG_DSN":     "postgres://test-user:test-password@localhost/test",
		"LF_MONGO_URI":  "mongodb://test-user:test-password@localhost/test",
		"LF_JWT_SECRET": strings.Repeat("s", 32),
	}
}

func envLookup(t *testing.T, values map[string]string) func(string) (string, bool) {
	t.Helper()
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}

func TestEnvLoaderDefaults(t *testing.T) {
	for _, state := range []string{"unset", "empty", "spaces"} {
		t.Run(state, func(t *testing.T) {
			values := validEnv(t)
			for _, key := range []string{"LF_HTTP_ADDR", "LF_ADMIN_ADDR", "LF_TCP_ADDR", "LF_CACHE_SIZE", "LF_LOG_LEVEL"} {
				switch state {
				case "empty":
					values[key] = ""
				case "spaces":
					values[key] = " \t\n "
				}
			}
			got, err := NewEnvLoader(envLookup(t, values)).Load(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			want := Config{
				HTTPAddr: ":8080", AdminAddr: "127.0.0.1:6060", TCPAddr: ":7070",
				PGDSN: values["LF_PG_DSN"], MongoURI: values["LF_MONGO_URI"], JWTSecret: values["LF_JWT_SECRET"],
				CacheSize: 100000, LogLevel: slog.LevelInfo,
			}
			if got != want {
				t.Error("configuration does not match defaults")
			}
		})
	}
}

func TestEnvLoaderOverrides(t *testing.T) {
	values := map[string]string{
		"LF_HTTP_ADDR": " \t[::1]:1\n", "LF_ADMIN_ADDR": " localhost:65535 ", "LF_TCP_ADDR": " :9000 ",
		"LF_PG_DSN": " opaque-dsn ", "LF_MONGO_URI": " opaque-uri ", "LF_JWT_SECRET": " " + strings.Repeat("я", 16) + " ",
		"LF_CACHE_SIZE": " 1 ", "LF_LOG_LEVEL": " DeBuG ",
	}
	got, err := NewEnvLoader(envLookup(t, values)).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		HTTPAddr: "[::1]:1", AdminAddr: "localhost:65535", TCPAddr: ":9000",
		PGDSN: "opaque-dsn", MongoURI: "opaque-uri", JWTSecret: strings.Repeat("я", 16),
		CacheSize: 1, LogLevel: slog.LevelDebug,
	}
	if got != want {
		t.Error("configuration overrides were not trimmed or parsed correctly")
	}
}

func TestEnvLoaderValidation(t *testing.T) {
	type testCase struct {
		name, key, value string
		unset            bool
		want             error
	}
	tests := []testCase{
		{name: "short JWT", key: "LF_JWT_SECRET", value: strings.Repeat("s", 31), want: ErrInvalid},
		{name: "short JWT after trimming", key: "LF_JWT_SECRET", value: " " + strings.Repeat("s", 31) + " ", want: ErrInvalid},
		{name: "bad cache number", key: "LF_CACHE_SIZE", value: "many", want: ErrInvalid},
		{name: "fractional cache", key: "LF_CACHE_SIZE", value: "1.5", want: ErrInvalid},
		{name: "zero cache", key: "LF_CACHE_SIZE", value: "0", want: ErrInvalid},
		{name: "negative cache", key: "LF_CACHE_SIZE", value: "-1", want: ErrInvalid},
		{name: "overflowing cache", key: "LF_CACHE_SIZE", value: strings.Repeat("9", 30), want: ErrInvalid},
		{name: "bad level", key: "LF_LOG_LEVEL", value: "trace", want: ErrInvalid},
	}
	for _, key := range []string{"LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET"} {
		tests = append(tests,
			testCase{name: key + " missing", key: key, unset: true, want: ErrMissing},
			testCase{name: key + " empty", key: key, value: "", want: ErrMissing},
			testCase{name: key + " blank", key: key, value: " \t\n ", want: ErrMissing},
		)
	}
	for _, key := range []string{"LF_HTTP_ADDR", "LF_ADMIN_ADDR", "LF_TCP_ADDR"} {
		for _, addr := range []string{"localhost", "::1:8080", ":", ":http", ":1.5", ":0", ":-1", ":65536", ":999999999999999999999999999999"} {
			tests = append(tests, testCase{name: key + " " + addr, key: key, value: addr, want: ErrInvalid})
		}
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := validEnv(t)
			if tc.unset {
				delete(values, tc.key)
			} else {
				values[tc.key] = tc.value
			}
			got, err := NewEnvLoader(envLookup(t, values)).Load(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want it to wrap %v", err, tc.want)
			}
			if !strings.HasPrefix(err.Error(), tc.key+": ") {
				t.Errorf("error must start with %s", tc.key)
			}
			if got != (Config{}) {
				t.Error("invalid configuration must return a zero Config")
			}
		})
	}
}

func TestEnvLoaderReportsAllProblems(t *testing.T) {
	values := map[string]string{
		"LF_HTTP_ADDR": "bad", "LF_ADMIN_ADDR": ":0", "LF_TCP_ADDR": ":65536",
		"LF_CACHE_SIZE": "bad", "LF_LOG_LEVEL": "bad",
	}
	got, err := NewEnvLoader(envLookup(t, values)).Load(context.Background())
	if !errors.Is(err, ErrMissing) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("error must wrap both missing and invalid errors: %v", err)
	}
	var joined interface{ Unwrap() []error }
	if !errors.As(err, &joined) {
		t.Fatal("errors must be joined")
	}
	if len(joined.Unwrap()) != 8 {
		t.Fatalf("got %d problems, want 8", len(joined.Unwrap()))
	}
	for _, key := range []string{"LF_HTTP_ADDR", "LF_ADMIN_ADDR", "LF_TCP_ADDR", "LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET", "LF_CACHE_SIZE", "LF_LOG_LEVEL"} {
		if strings.Count(err.Error(), key+": ") != 1 {
			t.Errorf("expected one problem for %s", key)
		}
	}
	if got != (Config{}) {
		t.Error("invalid configuration must return a zero Config")
	}
}

func TestEnvLoaderErrorsHideCredentials(t *testing.T) {
	values := validEnv(t)
	values["LF_JWT_SECRET"] = "short-private-test-secret"
	values["LF_HTTP_ADDR"] = values["LF_PG_DSN"]
	values["LF_CACHE_SIZE"] = values["LF_MONGO_URI"]
	values["LF_LOG_LEVEL"] = values["LF_JWT_SECRET"]
	_, err := NewEnvLoader(envLookup(t, values)).Load(context.Background())
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	for _, key := range []string{"LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET"} {
		if strings.Contains(err.Error(), values[key]) {
			t.Errorf("error exposed %s", key)
		}
	}
}

func TestEnvLoaderContext(t *testing.T) {
	for _, kind := range []string{"canceled", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if kind == "canceled" {
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			} else {
				ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
			}
			defer cancel()
			loader := NewEnvLoader(func(string) (string, bool) {
				t.Fatal("a canceled load must not read the environment")
				return "", false
			})
			got, err := loader.Load(ctx)
			if !errors.Is(err, ctx.Err()) || errors.Unwrap(err) != nil {
				t.Fatalf("error = %v, want the unwrapped context error %v", err, ctx.Err())
			}
			if got != (Config{}) {
				t.Error("canceled load must return a zero Config")
			}
		})
	}
}

func TestNewEnvLoaderNilLookup(t *testing.T) {
	loader := NewEnvLoader(nil)
	if loader.lookup == nil {
		t.Fatal("nil lookup must be replaced with os.LookupEnv")
	}
	// Exercise the fallback without depending on the process environment.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := loader.Load(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestEnvLoaderReadsEveryVariable(t *testing.T) {
	values := validEnv(t)
	reads := make(map[string]int)
	loader := NewEnvLoader(func(key string) (string, bool) {
		reads[key]++
		value, ok := values[key]
		return value, ok
	})
	if _, err := loader.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"LF_HTTP_ADDR": 1, "LF_ADMIN_ADDR": 1, "LF_TCP_ADDR": 1, "LF_PG_DSN": 1,
		"LF_MONGO_URI": 1, "LF_JWT_SECRET": 1, "LF_CACHE_SIZE": 1, "LF_LOG_LEVEL": 1,
	}
	if !maps.Equal(reads, want) {
		t.Errorf("environment reads = %v, want %v", reads, want)
	}
	values["LF_CACHE_SIZE"] = "42"
	got, err := loader.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.CacheSize != 42 {
		t.Errorf("CacheSize = %d, want fresh value 42", got.CacheSize)
	}
}

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug}, {"INFO", slog.LevelInfo},
		{" \tWaRn\n", slog.LevelWarn}, {" Error ", slog.LevelError},
	} {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseLevel(tc.input)
			if err != nil || got != tc.want {
				t.Fatalf("ParseLevel(%q) = %v, %v; want %v", tc.input, got, err, tc.want)
			}
		})
	}
	for _, input := range []string{"", " \t ", "trace", "warning", "INFO+1", "4"} {
		t.Run("invalid "+input, func(t *testing.T) {
			if _, err := ParseLevel(input); !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

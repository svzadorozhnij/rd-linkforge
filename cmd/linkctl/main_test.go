package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skskuzan/rd-linkforge/internal/link"
	"github.com/skskuzan/rd-linkforge/internal/linkio"
)

func lookupEnv(t *testing.T, values map[string]string) func(string) (string, bool) {
	t.Helper()
	return func(key string) (string, bool) { value, ok := values[key]; return value, ok }
}

func callCLI(t *testing.T, ctx context.Context, args []string, values map[string]string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(ctx, args, lookupEnv(t, values), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func writeTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func configEnv(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"LF_PG_DSN":     "postgres://test:private-password@localhost/test",
		"LF_MONGO_URI":  "mongodb://test:private-password@localhost/test",
		"LF_JWT_SECRET": "private-test-secret-with-32-bytes-minimum",
	}
}

func TestImportExport(t *testing.T) {
	dir := t.TempDir()
	input, db, output := filepath.Join(dir, "targets.txt"), filepath.Join(dir, "links.jsonl"), filepath.Join(dir, "links.csv")
	writeTestFile(t, input, "# targets\nhttps://example.com\n\nhttps://go.dev\n")
	args := []string{"import", "-in", input, "-db", db}
	code, stdout, stderr := callCLI(t, t.Context(), args, nil)
	if code != 0 || stdout != "imported: 2\nskipped: 0\n" {
		t.Fatalf("import: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	links, err := linkio.LoadFile(db)
	if err != nil || len(links) != 2 {
		t.Fatalf("database = %+v, %v", links, err)
	}
	for i, l := range links {
		if l.ID != uint64(i+1) || l.OwnerID != "cli" || l.CreatedAt.IsZero() || !l.ExpiresAt.IsZero() {
			t.Errorf("unexpected imported link: %+v", l)
		}
	}
	code, stdout, stderr = callCLI(t, t.Context(), []string{"export", "-db", db}, nil)
	if code != 0 || stderr != "" {
		t.Fatalf("export: code %d, stderr %q", code, stderr)
	}
	rows, err := csv.NewReader(strings.NewReader(stdout)).ReadAll()
	if err != nil || len(rows) != 3 || rows[1][0] != "1" || rows[1][2] != "https://example.com" || rows[2][0] != "2" {
		t.Fatalf("CSV rows = %v, %v", rows, err)
	}
	code, fileStdout, stderr := callCLI(t, t.Context(), []string{"export", "-db", db, "-out", output}, nil)
	if code != 0 || fileStdout != "" || readTestFile(t, output) != stdout {
		t.Fatalf("file export: code %d, stdout %q, stderr %q", code, fileStdout, stderr)
	}
	writeTestFile(t, input, "https://second.example\n")
	code, stdout, stderr = callCLI(t, t.Context(), args, nil)
	if code != 0 || stdout != "imported: 1\nskipped: 0\n" {
		t.Fatalf("second import: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	links, err = linkio.LoadFile(db)
	if err != nil || len(links) != 3 || links[2].ID != 3 || links[2].Code != "000003" {
		t.Fatalf("second database = %+v, %v", links, err)
	}
}

func TestImportUsesMaximumID(t *testing.T) {
	dir := t.TempDir()
	input, db := filepath.Join(dir, "targets"), filepath.Join(dir, "db")
	writeTestFile(t, input, "https://example.com")
	if err := linkio.SaveFile(db, []link.Link{{ID: 100, Code: "old-100"}, {ID: 3, Code: "old-3"}}); err != nil {
		t.Fatal(err)
	}
	code, stdout, stderr := callCLI(t, t.Context(), []string{"import", "-in", input, "-db", db}, nil)
	if code != 0 {
		t.Fatalf("import: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	links, err := linkio.LoadFile(db)
	if err != nil || len(links) != 3 || links[2].ID != 101 {
		t.Fatalf("database = %+v, %v; want new ID 101", links, err)
	}
}

func TestImportLoggingAndSkippedTargets(t *testing.T) {
	for _, level := range []string{"", "info", "debug", "warn", "error", " \t "} {
		t.Run("level "+level, func(t *testing.T) {
			dir := t.TempDir()
			input, db := filepath.Join(dir, "targets"), filepath.Join(dir, "db")
			writeTestFile(t, input, "# heading\nhttps://user:private-password@example.com/?token=private-token\nftp://user:private-password@private-host\nhttps://go.dev\n")
			code, stdout, stderr := callCLI(t, t.Context(), []string{"import", "-in", input, "-db", db}, map[string]string{"LF_LOG_LEVEL": level})
			if code != 1 || stdout != "imported: 2\nskipped: 1\n" {
				t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			links, err := linkio.LoadFile(db)
			if err != nil || len(links) != 2 || links[1].ID != 2 {
				t.Fatalf("good targets were not saved without consuming a skipped ID: %+v, %v", links, err)
			}
			for _, private := range []string{"https://", "ftp://", "private-password", "private-token", "private-host"} {
				if strings.Contains(stderr, private) {
					t.Errorf("log leaked %q", private)
				}
			}
			infoEnabled := level == "" || level == "info" || level == "debug" || strings.TrimSpace(level) == ""
			if strings.Contains(stderr, "import started") != infoEnabled || strings.Contains(stderr, "import finished") != infoEnabled {
				t.Errorf("unexpected info logs: %q", stderr)
			}
			if strings.Contains(stderr, "target skipped") != (level != "error") {
				t.Errorf("unexpected warn logs: %q", stderr)
			}
			if level != "error" && (!strings.Contains(stderr, "level=WARN") || !strings.Contains(stderr, "line=3") || !strings.Contains(stderr, "error=")) {
				t.Errorf("warning lacks structured attributes: %q", stderr)
			}
			if got := strings.Count(stderr, "target imported"); (level == "debug" && got != 2) || (level != "debug" && got != 0) {
				t.Errorf("unexpected debug logs: %q", stderr)
			}
		})
	}
}

func TestUsageErrors(t *testing.T) {
	tests := [][]string{
		nil, {"unknown"}, {"import"}, {"import", "-in", "file"}, {"import", "-db", "file"},
		{"import", "-in=", "-db=file"}, {"import", "-in=file", "-db= "}, {"export"}, {"export", "-db="},
		{"import", "-in"}, {"import", "-db"}, {"export", "-db"}, {"export", "-out"},
		{"import", "-in=file", "-db=file", "extra"}, {"export", "-db=file", "extra"}, {"config", "extra"}, {"version", "extra"},
	}
	for _, command := range []string{"import", "export", "config", "version"} {
		tests = append(tests, []string{command, "-unknown"}, []string{command, "-h"})
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := callCLI(t, t.Context(), args, nil)
			if code != 2 || stdout != "" || stderr == "" {
				t.Fatalf("code %d, stdout %q, stderr %q; want usage error", code, stdout, stderr)
			}
		})
	}
	code, stdout, stderr := callCLI(t, t.Context(), []string{"version"}, map[string]string{"LF_LOG_LEVEL": "private-invalid-level"})
	if code != 2 || stdout != "" || !strings.Contains(stderr, "LF_LOG_LEVEL") || strings.Contains(stderr, "private-invalid-level") {
		t.Fatalf("invalid log level: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestConfigCommand(t *testing.T) {
	values := configEnv(t)
	code, stdout, stderr := callCLI(t, t.Context(), []string{"config"}, values)
	if code != 0 || stdout != "config ok\n" || strings.Count(stderr, "[redacted]") != 3 {
		t.Fatalf("config: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for key, secret := range values {
		if strings.Contains(stderr, secret) {
			t.Errorf("log exposed %s", key)
		}
	}
	values["LF_JWT_SECRET"] = "too-short-private-secret"
	delete(values, "LF_PG_DSN")
	delete(values, "LF_MONGO_URI")
	code, stdout, stderr = callCLI(t, t.Context(), []string{"config"}, values)
	if code != 1 || stdout != "" || !strings.Contains(stderr, "invalid configuration:") {
		t.Fatalf("bad config: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	for _, key := range []string{"LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET"} {
		if !strings.Contains(stderr, key) {
			t.Errorf("missing problem for %s", key)
		}
	}
	if strings.Contains(stderr, values["LF_JWT_SECRET"]) {
		t.Fatal("error leaked a secret")
	}
	t.Run("short opaque credentials preserve log keys", func(t *testing.T) {
		values := configEnv(t)
		values["LF_PG_DSN"], values["LF_MONGO_URI"] = "dsn", "uri"
		code, stdout, stderr := callCLI(t, t.Context(), []string{"config"}, values)
		if code != 0 || stdout != "config ok\n" || strings.Count(stderr, "[redacted]") != 3 {
			t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		for _, key := range []string{"config.pg_dsn=", "config.mongo_uri=", "config.jwt_secret="} {
			if !strings.Contains(stderr, key) {
				t.Errorf("redaction changed log key %q", key)
			}
		}
	})
}

func TestDiagnosticsHideCredentials(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "bad-db")
	values := configEnv(t)
	for _, private := range []string{
		values["LF_PG_DSN"], values["LF_MONGO_URI"], values["LF_JWT_SECRET"],
		"https://private-user:private-password@private-host/path",
	} {
		t.Run(private, func(t *testing.T) {
			data, err := json.Marshal(map[string]string{"CreatedAt": private})
			if err != nil {
				t.Fatal(err)
			}
			writeTestFile(t, db, string(data))
			code, stdout, stderr := callCLI(t, t.Context(), []string{"export", "-db", db}, values)
			if code != 1 || stdout != "" || stderr == "" {
				t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
			if strings.Contains(stderr, private) || strings.Contains(stderr, "private-password") {
				t.Fatal("a parser diagnostic leaked credentials or a URI")
			}
		})
	}
}

func TestVersion(t *testing.T) {
	code, stdout, stderr := callCLI(t, t.Context(), []string{"version"}, nil)
	if code != 0 || stdout != "linkctl "+version+"\n" || stderr != "" {
		t.Fatalf("version: code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

type observingWriter struct {
	buffer  bytes.Buffer
	observe func([]byte)
}

func (w *observingWriter) Write(p []byte) (int, error) {
	n, err := w.buffer.Write(p)
	w.observe(p)
	return n, err
}

func TestCanceledImportLeavesDatabaseUntouched(t *testing.T) {
	for _, stage := range []string{"before run", "import started", "target imported", "target skipped"} {
		t.Run(stage, func(t *testing.T) {
			dir := t.TempDir()
			input, db := filepath.Join(dir, "targets"), filepath.Join(dir, "db")
			writeTestFile(t, input, "https://example.com\nftp://bad\nhttps://go.dev\n")
			if err := linkio.SaveFile(db, []link.Link{{ID: 10, Code: "existing", OwnerID: "owner"}}); err != nil {
				t.Fatal(err)
			}
			before := readTestFile(t, db)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if stage == "before run" {
				cancel()
			}
			stderr := &observingWriter{observe: func(p []byte) {
				if bytes.Contains(p, []byte(stage)) {
					cancel()
				}
			}}
			var stdout bytes.Buffer
			code := run(ctx, []string{"import", "-in", input, "-db", db}, lookupEnv(t, map[string]string{"LF_LOG_LEVEL": "debug"}), &stdout, stderr)
			if code != 1 || stdout.Len() != 0 || readTestFile(t, db) != before || !strings.Contains(stderr.buffer.String(), "context canceled") {
				t.Fatalf("canceled import: code %d, stdout %q, stderr %q", code, stdout.String(), stderr.buffer.String())
			}
		})
	}
	t.Run("missing database stays missing", func(t *testing.T) {
		dir := t.TempDir()
		input, db := filepath.Join(dir, "targets"), filepath.Join(dir, "db")
		writeTestFile(t, input, "https://example.com")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		stderr := &observingWriter{observe: func(p []byte) {
			if bytes.Contains(p, []byte("target imported")) {
				cancel()
			}
		}}
		var stdout bytes.Buffer
		code := run(ctx, []string{"import", "-in", input, "-db", db}, lookupEnv(t, map[string]string{"LF_LOG_LEVEL": "debug"}), &stdout, stderr)
		if code != 1 || stdout.Len() != 0 {
			t.Fatalf("code %d, stdout %q", code, stdout.String())
		}
		if _, err := os.Stat(db); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("canceled import created a database: %v", err)
		}
	})
}

func TestRuntimeErrors(t *testing.T) {
	dir := t.TempDir()
	input, db, badDB := filepath.Join(dir, "targets"), filepath.Join(dir, "db"), filepath.Join(dir, "bad-db")
	writeTestFile(t, input, "https://example.com")
	writeTestFile(t, db, "")
	writeTestFile(t, badDB, "not JSON")
	duplicateID, duplicateCode, exhausted := filepath.Join(dir, "duplicate-id"), filepath.Join(dir, "duplicate-code"), filepath.Join(dir, "exhausted")
	for path, links := range map[string][]link.Link{
		duplicateID:   {{ID: 1, Code: "one"}, {ID: 1, Code: "two"}},
		duplicateCode: {{ID: 1, Code: "same"}, {ID: 2, Code: "same"}},
		exhausted:     {{ID: ^uint64(0), Code: "last"}},
	} {
		if err := linkio.SaveFile(path, links); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing input", []string{"import", "-in", filepath.Join(dir, "missing"), "-db", db}},
		{"input directory", []string{"import", "-in", dir, "-db", db}},
		{"bad database import", []string{"import", "-in", input, "-db", badDB}},
		{"database directory", []string{"import", "-in", input, "-db", dir}},
		{"duplicate ID", []string{"import", "-in", input, "-db", duplicateID}},
		{"duplicate code", []string{"import", "-in", input, "-db", duplicateCode}},
		{"ID exhausted", []string{"import", "-in", input, "-db", exhausted}},
		{"save failure", []string{"import", "-in", input, "-db", filepath.Join(dir, "missing", "db")}},
		{"missing export database", []string{"export", "-db", filepath.Join(dir, "missing")}},
		{"bad export database", []string{"export", "-db", badDB}},
		{"output directory", []string{"export", "-db", db, "-out", dir}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr := callCLI(t, t.Context(), tc.args, nil)
			if code != 1 || stdout != "" || stderr == "" {
				t.Fatalf("code %d, stdout %q, stderr %q", code, stdout, stderr)
			}
		})
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestOutputErrors(t *testing.T) {
	dir := t.TempDir()
	input, db := filepath.Join(dir, "targets"), filepath.Join(dir, "db")
	writeTestFile(t, input, "https://example.com")
	writeTestFile(t, db, "")
	for _, args := range [][]string{
		{"version"}, {"config"}, {"import", "-in", input, "-db", db}, {"export", "-db", db},
	} {
		t.Run(args[0], func(t *testing.T) {
			var stderr bytes.Buffer
			code := run(t.Context(), args, lookupEnv(t, configEnv(t)), brokenWriter{}, &stderr)
			if code != 1 || !strings.Contains(stderr.String(), "closed pipe") {
				t.Fatalf("code %d, stderr %q", code, stderr.String())
			}
		})
	}
}

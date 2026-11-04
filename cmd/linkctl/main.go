// Command linkctl is the Linkforge operator CLI.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"

	"github.com/skskuzan/rd-linkforge/internal/config"
	"github.com/skskuzan/rd-linkforge/internal/link"
	"github.com/skskuzan/rd-linkforge/internal/linkio"
	"github.com/skskuzan/rd-linkforge/internal/store"
)

// version is overwritten at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

type options struct {
	command string
	input   string
	db      string
	output  string
}

func run(ctx context.Context, args []string, env func(string) (string, bool), stdout, stderr io.Writer) int {
	if env == nil {
		env = os.LookupEnv
	}
	diagnostics := newDiagnosticWriter(stderr, env)
	handlerOptions := &slog.HandlerOptions{ReplaceAttr: diagnostics.redactAttr}
	levelText, ok := env("LF_LOG_LEVEL")
	if !ok || strings.TrimSpace(levelText) == "" {
		levelText = "info"
	}
	level, err := config.ParseLevel(levelText)
	if err != nil {
		slog.New(slog.NewTextHandler(stderr, handlerOptions)).Error("LF_LOG_LEVEL: invalid log level")
		return 2
	}
	handlerOptions.Level = level
	logger := slog.New(slog.NewTextHandler(stderr, handlerOptions))
	opts, err := parseOptions(args, diagnostics)
	if err != nil {
		logger.Error("invalid command usage", "error", err)
		return 2
	}
	if err := ctx.Err(); err != nil {
		logger.Error("command canceled", "error", err)
		return 1
	}

	switch opts.command {
	case "import":
		imported, skipped, err := importLinks(ctx, opts, logger)
		if err != nil {
			logger.Error("import failed", "error", err)
			return 1
		}
		if _, err := fmt.Fprintf(stdout, "imported: %d\nskipped: %d\n", imported, skipped); err != nil {
			logger.Error("write import result", "error", err)
			return 1
		}
		if skipped > 0 {
			return 1
		}
	case "export":
		if err := exportLinks(ctx, opts, stdout); err != nil {
			logger.Error("export failed", "error", err)
			return 1
		}
	case "config":
		cfg, err := config.NewEnvLoader(env).Load(ctx)
		if err != nil {
			logger.Error("invalid configuration:", "error", err)
			return 1
		}
		logger.Info("configuration loaded", "config", cfg)
		if _, err := fmt.Fprintln(stdout, "config ok"); err != nil {
			logger.Error("write config result", "error", err)
			return 1
		}
	case "version":
		if _, err := fmt.Fprintf(stdout, "linkctl %s\n", version); err != nil {
			logger.Error("write version", "error", err)
			return 1
		}
	}
	return 0
}

type diagnosticWriter struct {
	output  io.Writer
	secrets *strings.Replacer
	uris    *regexp.Regexp
}

func newDiagnosticWriter(output io.Writer, env func(string) (string, bool)) *diagnosticWriter {
	var replacements []string
	for _, key := range []string{"LF_PG_DSN", "LF_MONGO_URI", "LF_JWT_SECRET"} {
		if value, ok := env(key); ok && strings.TrimSpace(value) != "" {
			replacements = append(replacements, value, "[redacted]", strings.TrimSpace(value), "[redacted]")
		}
	}
	return &diagnosticWriter{
		output: output, secrets: strings.NewReplacer(replacements...),
		uris: regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s"'<>\\]+`),
	}
}

func (w *diagnosticWriter) Write(p []byte) (int, error) {
	// Parser and flag errors may echo input, even when our own messages do not.
	text := w.redact(string(p))
	n, err := io.WriteString(w.output, text)
	if err != nil {
		return 0, err
	}
	if n != len(text) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func (w *diagnosticWriter) redact(text string) string {
	return w.uris.ReplaceAllString(w.secrets.Replace(text), "[redacted]")
}

func (w *diagnosticWriter) redactAttr(_ []string, attr slog.Attr) slog.Attr {
	if err, ok := attr.Value.Any().(error); ok {
		attr.Value = slog.StringValue(w.redact(err.Error()))
	} else if attr.Value.Kind() == slog.KindString {
		attr.Value = slog.StringValue(w.redact(attr.Value.String()))
	}
	return attr
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	if len(args) == 0 {
		return options{}, errors.New("usage: linkctl {import -in FILE -db FILE | export -db FILE [-out FILE] | config | version}")
	}
	opts := options{command: args[0]}
	flags := flag.NewFlagSet(opts.command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	switch opts.command {
	case "import":
		flags.StringVar(&opts.input, "in", "", "target text file (required)")
		flags.StringVar(&opts.db, "db", "", "JSON Lines database (required)")
	case "export":
		flags.StringVar(&opts.db, "db", "", "JSON Lines database (required)")
		flags.StringVar(&opts.output, "out", "", "CSV output file (default stdout)")
	case "config", "version":
	default:
		return options{}, errors.New("unknown command; use import, export, config or version")
	}
	if err := flags.Parse(args[1:]); err != nil {
		return options{}, fmt.Errorf("parse %s flags: %w", opts.command, err)
	}
	if flags.NArg() != 0 {
		return options{}, errors.New("unexpected positional arguments")
	}
	if opts.command == "import" && (strings.TrimSpace(opts.input) == "" || strings.TrimSpace(opts.db) == "") {
		return options{}, errors.New("usage: linkctl import -in FILE -db FILE; both flags are required")
	}
	if opts.command == "export" && strings.TrimSpace(opts.db) == "" {
		return options{}, errors.New("usage: linkctl export -db FILE [-out FILE]; -db is required")
	}
	return opts, nil
}

func importLinks(ctx context.Context, opts options, logger *slog.Logger) (imported, skipped int, err error) {
	logger.Info("import started")
	defer func() { logger.Info("import finished", "imported", imported, "skipped", skipped) }()
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	targets, err := readTargetsFile(opts.input)
	if err != nil {
		return 0, 0, err
	}
	links, err := linkio.LoadFile(opts.db)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return 0, 0, err
	}
	st := store.New()
	var maxID uint64
	for _, l := range links {
		if _, err := st.Create(ctx, l); err != nil {
			return 0, 0, fmt.Errorf("restore database: %w", err)
		}
		maxID = max(maxID, l.ID)
	}
	if maxID == math.MaxUint64 {
		return 0, 0, errors.New("database ID space exhausted")
	}
	svc := link.NewService(st, link.WithFirstID(maxID+1))
	for _, target := range targets {
		l, err := svc.Shorten(ctx, link.ShortenRequest{Target: target.URL, OwnerID: "cli"})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return imported, skipped, ctxErr
		}
		if err != nil {
			skipped++
			logger.Warn("target skipped", "line", target.Line, "error", err)
			continue
		}
		imported++
		// Targets may contain passwords or tokens; identify them by line and ID only.
		logger.Debug("target imported", "line", target.Line, "id", l.ID)
	}
	if err := ctx.Err(); err != nil {
		return imported, skipped, err
	}
	if err := linkio.SaveFile(opts.db, st.All()); err != nil {
		return imported, skipped, err
	}
	return imported, skipped, nil
}

func readTargetsFile(path string) ([]linkio.Target, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open targets %q: %w", path, err)
	}
	targets, readErr := linkio.ReadTargets(file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("read targets %q: %w", path, err)
	}
	return targets, nil
}

func exportLinks(ctx context.Context, opts options, stdout io.Writer) error {
	links, err := linkio.LoadFile(opts.db)
	if err != nil {
		return fmt.Errorf("read database: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if opts.output == "" {
		return linkio.WriteCSV(stdout, links)
	}
	file, err := os.Create(opts.output)
	if err != nil {
		return fmt.Errorf("create export %q: %w", opts.output, err)
	}
	writeErr := linkio.WriteCSV(file, links)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("write export %q: %w", opts.output, err)
	}
	return nil
}

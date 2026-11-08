package linkio

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func sampleLinks(t *testing.T) []link.Link {
	t.Helper()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("test", 2*60*60))
	return []link.Link{
		{ID: 1, Code: "first", Target: "https://example.com/?a=1,b=2", OwnerID: "owner", CreatedAt: created},
		{ID: 2, Code: "second", Target: "https://go.dev", OwnerID: "cli", CreatedAt: created, ExpiresAt: created.Add(time.Hour)},
	}
}

func TestReadTargets(t *testing.T) {
	got, err := ReadTargets(strings.NewReader(" # comment\r\n\n https://example.com \r\n\t# another\n \t\nhttps://go.dev#fragment"))
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{{Line: 3, URL: "https://example.com"}, {Line: 6, URL: "https://go.dev#fragment"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
	if got, err := ReadTargets(strings.NewReader("")); err != nil || len(got) != 0 {
		t.Fatalf("empty input = %+v, %v", got, err)
	}
	long := "https://example.com/" + strings.Repeat("a", 100000)
	if got, err := ReadTargets(strings.NewReader(long)); err != nil || len(got) != 1 || got[0].URL != long {
		t.Fatal("long target was truncated or rejected")
	}
}

func TestLinksRoundTrip(t *testing.T) {
	links := sampleLinks(t)
	var output bytes.Buffer
	if err := WriteLinks(&output, links); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != len(links) {
		t.Fatal("expected one JSON object per line")
	}
	got, err := ReadLinks(&output)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(links) {
		t.Fatalf("got %d links, want %d", len(got), len(links))
	}
	for i := range got {
		if got[i].ID != links[i].ID || got[i].Code != links[i].Code || got[i].Target != links[i].Target || got[i].OwnerID != links[i].OwnerID || !got[i].CreatedAt.Equal(links[i].CreatedAt) || !got[i].ExpiresAt.Equal(links[i].ExpiresAt) {
			t.Errorf("record %d did not survive the round trip", i+1)
		}
	}
}

func TestReadLinksRecords(t *testing.T) {
	for _, input := range []string{"", "{}", "{}\n"} {
		t.Run("valid "+input, func(t *testing.T) {
			got, err := ReadLinks(strings.NewReader(input))
			if err != nil {
				t.Fatal(err)
			}
			if input == "" && len(got) != 0 {
				t.Fatal("empty input must return no links")
			}
		})
	}
	for _, input := range []string{"{}\n{}\n{", "{}\n{}\n{\"ID\":\"bad\"}", "{}\n{}\n{} {}", "{}\n{}\n\n", "{}\n{}\nnull", "{}\n{}\n[]"} {
		t.Run("invalid "+input, func(t *testing.T) {
			if _, err := ReadLinks(strings.NewReader(input)); err == nil || !strings.Contains(err.Error(), "record 3:") {
				t.Fatalf("error = %v, want record 3", err)
			}
		})
	}
	_, err := ReadLinks(strings.NewReader("{}\n{"))
	var syntaxErr *json.SyntaxError
	if !errors.As(err, &syntaxErr) {
		t.Fatalf("error does not wrap a JSON syntax error: %v", err)
	}
}

func TestWriteCSV(t *testing.T) {
	var output bytes.Buffer
	if err := WriteCSV(&output, sampleLinks(t)); err != nil {
		t.Fatal(err)
	}
	want := "id,code,target,owner_id,created_at,expires_at\n" +
		"1,first,\"https://example.com/?a=1,b=2\",owner,2026-01-02T01:04:05Z,\n" +
		"2,second,https://go.dev,cli,2026-01-02T01:04:05Z,2026-01-02T02:04:05Z\n"
	if output.String() != want {
		t.Fatalf("CSV = %q, want %q", output.String(), want)
	}
	output.Reset()
	if err := WriteCSV(&output, nil); err != nil || output.String() != "id,code,target,owner_id,created_at,expires_at\n" {
		t.Fatalf("empty CSV = %q, %v", output.String(), err)
	}
}

func TestStreamErrors(t *testing.T) {
	boom := errors.New("test I/O failure")
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"read targets", func() error { _, err := ReadTargets(failingReader{boom}); return err }},
		{"read links", func() error { _, err := ReadLinks(failingReader{boom}); return err }},
		{"write links", func() error { return WriteLinks(failingWriter{boom}, sampleLinks(t)) }},
		{"flush CSV", func() error { return WriteCSV(failingWriter{boom}, nil) }},
		{"write CSV row", func() error { return WriteCSV(failingWriter{boom}, []link.Link{{Target: strings.Repeat("x", 10000)}}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, boom) {
				t.Fatalf("error = %v, want wrapped I/O failure", err)
			}
		})
	}
	badTime := []link.Link{{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}
	if err := WriteLinks(io.Discard, badTime); err == nil {
		t.Fatal("unencodable time must fail")
	}
}

func TestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "links.jsonl")
	links := sampleLinks(t)
	if err := SaveFile(path, links); err != nil {
		t.Fatal(err)
	}
	got, err := LoadFile(path)
	if err != nil || len(got) != len(links) || got[0].Target != links[0].Target {
		t.Fatalf("loaded links = %+v, %v", got, err)
	}
	if err := SaveFile(path, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := LoadFile(path); err != nil || len(got) != 0 {
		t.Fatalf("replacement did not truncate old data: %+v, %v", got, err)
	}
	missing := filepath.Join(dir, "missing.jsonl")
	if _, err := LoadFile(missing); !errors.Is(err, fs.ErrNotExist) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing file error = %v", err)
	}
	if err := SaveFile(filepath.Join(dir, "missing", "links"), nil); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("save to missing directory error = %v", err)
	}
	if _, err := LoadFile(dir); err == nil || !strings.Contains(err.Error(), dir) {
		t.Fatalf("directory read error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("bad database error = %v", err)
	}
	if err := SaveFile(path, []link.Link{{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("bad database write error = %v", err)
	}
}

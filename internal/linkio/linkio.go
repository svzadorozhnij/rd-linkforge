// Package linkio reads and writes link import files and database snapshots.
package linkio

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

type Target struct {
	Line int
	URL  string
}

func ReadTargets(r io.Reader) ([]Target, error) {
	reader := bufio.NewReader(r)
	var targets []Target
	for line := 1; ; line++ {
		text, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read targets at line %d: %w", line, err)
		}
		text = strings.TrimSpace(text)
		if text != "" && !strings.HasPrefix(text, "#") {
			targets = append(targets, Target{Line: line, URL: text})
		}
		if errors.Is(err, io.EOF) {
			return targets, nil
		}
	}
}

func ReadLinks(r io.Reader) ([]link.Link, error) {
	reader := bufio.NewReader(r)
	var links []link.Link
	for record := 1; ; record++ {
		text, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read record %d: %w", record, err)
		}
		if len(text) > 0 {
			var l *link.Link
			if decodeErr := json.Unmarshal([]byte(text), &l); decodeErr != nil {
				return nil, fmt.Errorf("record %d: %w", record, decodeErr)
			}
			if l == nil {
				return nil, fmt.Errorf("record %d: expected a JSON object", record)
			}
			links = append(links, *l)
		}
		if errors.Is(err, io.EOF) {
			return links, nil
		}
	}
}

func WriteLinks(w io.Writer, links []link.Link) error {
	encoder := json.NewEncoder(w)
	for i, l := range links {
		if err := encoder.Encode(l); err != nil {
			return fmt.Errorf("write record %d: %w", i+1, err)
		}
	}
	return nil
}

func WriteCSV(w io.Writer, links []link.Link) error {
	writer := csv.NewWriter(w)
	if err := writer.Write([]string{"id", "code", "target", "owner_id", "created_at", "expires_at"}); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	for _, l := range links {
		expiresAt := ""
		if !l.ExpiresAt.IsZero() {
			expiresAt = l.ExpiresAt.UTC().Format(time.RFC3339)
		}
		if err := writer.Write([]string{
			strconv.FormatUint(l.ID, 10), l.Code, l.Target, l.OwnerID,
			l.CreatedAt.UTC().Format(time.RFC3339), expiresAt,
		}); err != nil {
			return fmt.Errorf("write CSV row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush CSV: %w", err)
	}
	return nil
}

func LoadFile(path string) ([]link.Link, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("load %q: %w", path, err)
	}
	links, readErr := ReadLinks(file)
	if err := errors.Join(readErr, file.Close()); err != nil {
		return nil, fmt.Errorf("load %q: %w", path, err)
	}
	return links, nil
}

func SaveFile(path string, links []link.Link) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	writeErr := WriteLinks(file, links)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return fmt.Errorf("save %q: %w", path, err)
	}
	return nil
}

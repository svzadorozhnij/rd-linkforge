// Package link holds the domain model and the contracts its consumers require.
// It may not import a transport or a storage package.
package link

import (
	"errors"
	"strings"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/base62"
)

// Link is a shortened URL. A zero ExpiresAt means it never expires.
type Link struct {
	ID        int64
	Code      string
	TargetURL string
	OwnerID   string
	CreatedAt time.Time
	ExpiresAt time.Time
}

func New(id uint64, target string) (Link, error) {
	if len(target) == 0 {
		return Link{}, errors.New("ErrEmptyTarget: " + target)
	}

	allowsPrefixes := []string{"http://", "https://"}

	hasError := true

	for i := range allowsPrefixes {
		if strings.HasPrefix(target, allowsPrefixes[i]) {
			hasError = false
		}
	}
	if hasError {
		return Link{}, errors.New("ErrUnsupportedScheme: " + target)
	}
	result := Link{ID: int64(id),
		Code:      base62.EncodeWidth(id, 6),
		TargetURL: target,
		CreatedAt: time.Now().UTC(),
	}

	return result, nil
}

// ShortenRequest carries everything needed to create a link. An empty Alias
// asks for a generated code.
type ShortenRequest struct {
	TargetURL string
	Alias     string
	OwnerID   string
	ExpiresAt time.Time
}

// Page describes one slice of a cursor-paginated listing.
type Page struct {
	Cursor string
	Limit  int
}

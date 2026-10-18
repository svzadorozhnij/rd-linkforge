// Package link holds the domain model and the contracts its consumers require.
// It may not import a transport or a storage package.
package link

import (
	"errors"
	"strings"
	"time"

	"github.com/skskuzan/rd-linkforge/internal/base62"
)

var (
	ErrEmptyTarget       = errors.New("link: empty target")
	ErrUnsupportedScheme = errors.New("link: unsupported scheme")
	ErrNotFound          = errors.New("link: not found")
	ErrAliasTaken        = errors.New("link: alias already taken")
	ErrInvalidAlias      = errors.New("link: invalid alias")
	ErrReservedAlias     = errors.New("link: reserved alias")
	ErrForbidden         = errors.New("link: forbidden")
	ErrExpired           = errors.New("link: expired")
	ErrInvalidExpiry     = errors.New("link: invalid expiry")
	ErrEmptyOwner        = errors.New("link: owner is required")
	ErrInvalidCursor     = errors.New("link: invalid cursor")
)

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 100
)

// Link is a shortened URL. A zero ExpiresAt means it never expires.
type Link struct {
	ID             int64
	Code           string
	TargetURL      string
	OwnerID        string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	ShortenRequest ShortenRequest
	Page           Page
}

func New(id uint64, target string) (Link, error) {
	if len(target) == 0 {
		return Link{}, ErrEmptyTarget
	}

	allowsPrefixes := []string{"http://", "https://"}

	hasError := true

	for i := range allowsPrefixes {
		if strings.HasPrefix(target, allowsPrefixes[i]) {
			hasError = false
		}
	}
	if hasError {
		return Link{}, ErrUnsupportedScheme
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

func (p Page) Normalize() Page {
	if p.Limit <= 0 {
		return Page{Limit: DefaultPageLimit}
	} else if p.Limit > MaxPageLimit {
		return Page{Limit: MaxPageLimit}
	}
	return Page{Limit: p.Limit}
}

func (p Page) AfterID() (uint64, error) {
	if p.Cursor == "" {
		return 0, ErrInvalidCursor
	} else if v, err := base62.Decode(p.Cursor); err != nil {
		return 0, ErrInvalidCursor
	} else {
		return v, nil
	}
}

type AliasError struct {
	Alias string
	Err   error
}

func (e *AliasError) Error() string {
	return e.Err.Error()
}
func (e *AliasError) Unwrap() error {
	return e.Err
}

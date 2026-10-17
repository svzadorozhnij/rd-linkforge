// Package link holds the domain model and the contracts its consumers require.
// It may not import a transport or a storage package.
package link

import (
	"testing"

	"github.com/skskuzan/rd-linkforge/internal/base62"
)

func TestLinkModel(t *testing.T) {

	invalids := map[uint64]string{4322245: "google.com", 123: ""}
	correctValues := map[uint64]string{324: "http://test.ua", 124: "https://124.com", 324423: "http://google.com"}

	for k, v := range invalids {
		newLink, err := New(k, v)
		if err == nil {
			t.Fatalf("New(%d, %s): %v", k, v, newLink)
		}
	}

	for k, v := range correctValues {
		newLink, err := New(k, v)

		if err != nil {
			t.Fatalf("New(%d, %s) -> Error %v", k, v, err)
		}

		if newLink.TargetURL != v || base62.EncodeWidth(k, 6) != newLink.Code {
			t.Fatalf("New(%d, %s): %v", k, v, newLink)
		}
	}
}

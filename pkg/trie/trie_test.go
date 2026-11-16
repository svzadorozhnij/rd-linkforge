package trie_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/skskuzan/rd-linkforge/pkg/trie"
)

func TestTrieInsertAndContains(t *testing.T) {
	tree := trie.New()
	if tree.Insert("") || tree.Contains("") || tree.Delete("") {
		t.Fatal("the empty string must never be a word")
	}
	if tree.Contains("doc") || tree.Delete("doc") {
		t.Fatal("an empty trie contains a word")
	}
	for _, word := range []string{"docs", "doc", "dog", "do-it", "day", "api", "dock"} {
		if !tree.Insert(word) || !tree.Contains(word) {
			t.Fatalf("failed to insert/find %q", word)
		}
		before := tree.WithPrefix("", 0)
		if tree.Insert(word) || !slices.Equal(before, tree.WithPrefix("", 0)) {
			t.Fatalf("duplicate insertion of %q changed the trie", word)
		}
	}
	for _, word := range []string{"", "do", "docx", "docks", "unknown"} {
		if tree.Contains(word) {
			t.Errorf("Contains(%q) = true", word)
		}
	}
}

func TestTrieWithPrefix(t *testing.T) {
	tree := trie.New()
	if got := tree.WithPrefix("", 0); len(got) != 0 {
		t.Fatalf("empty trie returned %v", got)
	}
	for _, word := range []string{"docs", "doc", "dog", "do-it", "day", "api", "dock"} {
		tree.Insert(word)
	}
	for _, tc := range []struct {
		prefix string
		limit  int
		want   []string
	}{
		{"do", 0, []string{"do-it", "doc", "dock", "docs", "dog"}},
		{"do", -1, []string{"do-it", "doc", "dock", "docs", "dog"}},
		{"do", 2, []string{"do-it", "doc"}},
		{"do", 100, []string{"do-it", "doc", "dock", "docs", "dog"}},
		{"doc", 1, []string{"doc"}},
		{"doc", 0, []string{"doc", "dock", "docs"}},
		{"docs", 0, []string{"docs"}},
		{"", 0, []string{"api", "day", "do-it", "doc", "dock", "docs", "dog"}},
		{"", 1, []string{"api"}},
		{"unknown", 0, nil},
	} {
		got := tree.WithPrefix(tc.prefix, tc.limit)
		if !slices.Equal(got, tc.want) {
			t.Errorf("WithPrefix(%q, %d) = %q, want %q", tc.prefix, tc.limit, got, tc.want)
		}
	}
}

func TestTrieDelete(t *testing.T) {
	tree := trie.New()
	for _, word := range []string{"doc", "docs", "dock", "dog", "api"} {
		tree.Insert(word)
	}
	if tree.Delete("do") || tree.Delete("docks") || tree.Delete("unknown") {
		t.Fatal("deleting a missing word succeeded")
	}
	if !tree.Delete("doc") || tree.Contains("doc") || tree.Delete("doc") {
		t.Fatal("deleting the prefix word failed")
	}
	if got := tree.WithPrefix("do", 0); !slices.Equal(got, []string{"dock", "docs", "dog"}) {
		t.Fatalf("deleting a prefix damaged longer words: %q", got)
	}
	for _, word := range []string{"dock", "docs", "dog", "api"} {
		if !tree.Delete(word) || tree.Contains(word) {
			t.Fatalf("failed to delete %q", word)
		}
	}
	if got := tree.WithPrefix("", 0); len(got) != 0 {
		t.Fatalf("words remain after deletion: %q", got)
	}
	if !tree.Insert("docs") || !tree.Insert("doc") || !tree.Delete("docs") || !tree.Contains("doc") {
		t.Fatal("reinsertion or deleting a longer word damaged the prefix")
	}
}

func TestTrieByteOrderAndZeroValue(t *testing.T) {
	var tree trie.Trie
	words := []string{"é", "a\xff", "a\x00", "a", "\xff", "\x00", "є", "A"}
	for _, word := range words {
		if !tree.Insert(word) || !tree.Contains(word) {
			t.Fatalf("failed to store bytes %q", word)
		}
	}
	slices.Sort(words)
	if got := tree.WithPrefix("", 0); !slices.Equal(got, words) {
		t.Fatalf("byte order = %q, want %q", got, words)
	}
	// Prefixes can end inside a UTF-8 sequence: the trie operates on bytes.
	if got := tree.WithPrefix("\xc3", 0); !slices.Equal(got, []string{"é"}) {
		t.Fatalf("byte prefix returned %q", got)
	}
	for _, word := range words {
		if !tree.Delete(word) {
			t.Errorf("failed to delete bytes %q", word)
		}
	}
}

func BenchmarkWithPrefix(b *testing.B) {
	tree := trie.New()
	for i := range 10_000 {
		tree.Insert(fmt.Sprintf("alias%05d", i))
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := tree.WithPrefix("alias09", 20); len(got) != 20 {
			b.Fatalf("got %d matches, want 20", len(got))
		}
	}
}

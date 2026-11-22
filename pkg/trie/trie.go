// Package trie provides byte-ordered prefix lookup for aliases.
package trie

// Trie stores nonempty words. It is not safe for concurrent use.
// Its zero value is ready to use.
type Trie struct {
	root node
}

type node struct {
	children map[byte]*node
	word     bool
}

// New returns an empty trie.
func New() *Trie { return &Trie{} }

// Insert returns false without changing the trie for an empty or existing word.
func (t *Trie) Insert(word string) bool {
	if word == "" {
		return false
	}
	n := &t.root
	for i := range len(word) {
		if n.children == nil {
			n.children = make(map[byte]*node)
		}
		child := n.children[word[i]]
		if child == nil {
			child = &node{}
			n.children[word[i]] = child
		}
		n = child
	}
	if n.word {
		return false
	}
	n.word = true
	return true
}

// Contains reports whether word was inserted and has not been deleted.
func (t *Trie) Contains(word string) bool {
	if word == "" {
		return false
	}
	n := t.find(word)
	return n != nil && n.word
}

// Delete removes a word while retaining any words that extend it.
func (t *Trie) Delete(word string) bool {
	if word == "" {
		return false
	}
	return t.root.remove(word, 0)
}

func (n *node) remove(word string, depth int) bool {
	if depth == len(word) {
		if !n.word {
			return false
		}
		n.word = false
		return true
	}
	child := n.children[word[depth]]
	if child == nil || !child.remove(word, depth+1) {
		return false
	}
	// Prune unused branches so repeated insert/delete cycles do not retain them.
	if !child.word && len(child.children) == 0 {
		delete(n.children, word[depth])
	}
	return true
}

func (t *Trie) find(prefix string) *node {
	n := &t.root
	for i := range len(prefix) {
		n = n.children[prefix[i]]
		if n == nil {
			return nil
		}
	}
	return n
}

// WithPrefix returns matching words in ascending byte order. A nonpositive
// limit returns all matches; the empty prefix matches every stored word.
func (t *Trie) WithPrefix(prefix string, limit int) []string {
	n := t.find(prefix)
	if n == nil {
		return nil
	}
	var words []string
	path := []byte(prefix)
	var visit func(*node) bool
	visit = func(n *node) bool {
		if n.word {
			words = append(words, string(path))
			if limit > 0 && len(words) == limit {
				return true
			}
		}
		// A byte has only 256 possible values. Walking them in order avoids
		// sorting map keys or collecting matches beyond the requested limit.
		for b := 0; b < 256; b++ {
			if child := n.children[byte(b)]; child != nil {
				path = append(path, byte(b))
				stop := visit(child)
				path = path[:len(path)-1]
				if stop {
					return true
				}
			}
		}
		return false
	}
	visit(n)
	return words
}

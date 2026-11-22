package shortid

import (
	"fmt"
	"io"
)

// CodeGenerator combines 19 random bits with a wrapping 16-bit counter.
// It is not safe for concurrent use. Construct it with NewCodeGenerator.
type CodeGenerator struct {
	random  io.Reader
	counter uint16
}

var _ Generator = (*CodeGenerator)(nil)

// NewCodeGenerator takes the source of random bytes, normally crypto/rand.Reader.
func NewCodeGenerator(random io.Reader) *CodeGenerator {
	return &CodeGenerator{random: random}
}

// Next returns a six-character base62 code. A read failure leaves the counter
// unchanged and is returned with its original cause wrapped.
func (g *CodeGenerator) Next() (string, error) {
	var data [3]byte
	if _, err := io.ReadFull(g.random, data[:]); err != nil {
		return "", fmt.Errorf("shortid: read random bytes: %w", err)
	}
	random := (uint64(data[0])<<16 | uint64(data[1])<<8 | uint64(data[2])) & ((1 << 19) - 1)
	code := encode(random<<16 | uint64(g.counter))
	g.counter++
	return code, nil
}

func encode(value uint64) string {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	var code [6]byte
	for i := len(code) - 1; i >= 0; i-- {
		code[i] = alphabet[value%62]
		value /= 62
	}
	return string(code[:])
}

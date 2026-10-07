package base62

import (
	"errors"
	"math"
	"strings"
)

const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

var (
	ErrEmpty            = errors.New("base62: empty code")
	ErrInvalidCharacter = errors.New("base62: invalid character")
	ErrOverflow         = errors.New("base62: code is too long")
)

func Encode(v uint64) string {
	if v <= 0 {
		return "0"
	}

	var preResult strings.Builder

	for v > 0 {
		currentIndex := v % 62
		preResult.WriteByte(alphabet[currentIndex])
		v = v / 62
	}

	return reverse(preResult.String())
}

func reverse(preResult string) string {
	var result string

	for _, v := range preResult {
		result = string(v) + result
	}

	return result
}

func EncodeWidth(v uint64, width int) string {
	if encoded := Encode(v); len(encoded) < width {
		return strings.Repeat("0", width-len(encoded)) + encoded
	} else {
		return encoded
	}
}

func Decode(s string) (uint64, error) {

	if len(s) == 0 {
		return 0, ErrEmpty
	}

	result := uint64(0)

	for v := range strings.SplitSeq(s, "") {
		currentIndex := strings.IndexAny(alphabet, v)

		if currentIndex == -1 {
			return 0, ErrInvalidCharacter
		}

		if result > (math.MaxUint64-uint64(currentIndex))/62 {
			return 0, ErrOverflow
		}

		result = result*62 + uint64(currentIndex)
	}

	return result, nil
}

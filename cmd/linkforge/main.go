package main

import (
	"fmt"

	"github.com/skskuzan/rd-linkforge/internal/base62"
)

var version = "dev"

func main() {
	fmt.Printf("linkforge %s\n", version)
	fmt.Println(base62.Encode(34))

	if v, er := base62.Decode("Y"); er == nil {
		fmt.Println(v)
	}
}

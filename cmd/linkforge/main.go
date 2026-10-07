package main

import (
	"fmt"
	"os"

	"github.com/skskuzan/rd-linkforge/internal/link"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("\nTotal: 0")
		return
	}
	var urlsCount int
	for i, url := range args {
		if res, err := link.New(uint64(i+1), url); err == nil {
			fmt.Printf("%s %s\n", res.Code, res.TargetURL)
			urlsCount++
		}
	}

	fmt.Printf("\nTotal: %d\n", urlsCount)
}

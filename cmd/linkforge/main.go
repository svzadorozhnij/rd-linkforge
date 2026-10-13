package main

import (
	"fmt"
	"os"

	"github.com/skskuzan/rd-linkforge/internal/link"
	"github.com/skskuzan/rd-linkforge/internal/store"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Println("\nTotal: 0")
		return
	}
	var storedLinks = store.New()
	for i, url := range args {
		if res, err := link.New(uint64(i+1), url); err == nil {
			err = store.Add(storedLinks, res)
		} else {
			_, _ = os.Stderr.WriteString(fmt.Sprintf("%s -> %s\n", err, url))
		}
	}

	for _, curLink := range store.All(storedLinks) {
		fmt.Printf("%s %s\n", curLink.Code, curLink.TargetURL)
	}

	fmt.Printf("\nTotal: %d\n", store.Count(storedLinks))
}

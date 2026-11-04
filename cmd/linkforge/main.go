package main

import (
	"context"
	"fmt"
	"os"

	"github.com/skskuzan/rd-linkforge/internal/link"
	"github.com/skskuzan/rd-linkforge/internal/store"
)

func main() {
	ctx := context.Background()
	storedLinks := store.New()
	svc := link.NewService(storedLinks)
	for _, target := range os.Args[1:] {
		if _, err := svc.Shorten(ctx, link.ShortenRequest{Target: target, OwnerID: "demo"}); err != nil {
			fmt.Fprintf(os.Stderr, "%s -> %s\n", target, err)
		}
	}

	for _, curLink := range storedLinks.All() {
		fmt.Printf("%s %s\n", curLink.Code, curLink.Target)
	}

	fmt.Printf("Total: %d\n", storedLinks.Count())
}

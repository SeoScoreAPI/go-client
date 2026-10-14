// Audit one URL and print its score and top fixes.
//
//	SEOSCORE_API_KEY=ssa_... go run ./examples/audit https://example.com
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	seoscoreapi "github.com/SeoScoreAPI/go-client"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: audit <url>")
	}
	client := seoscoreapi.New() // reads SEOSCORE_API_KEY

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := client.Audit(ctx, os.Args[1], nil)
	switch {
	case errors.Is(err, seoscoreapi.ErrQuotaExceeded):
		log.Fatal("audit allowance used up; see https://seoscoreapi.com/pricing")
	case err != nil:
		log.Fatal(err)
	}

	fmt.Printf("%s: %.1f/100 (%s)\n", result.URL, result.Score, result.Grade)
	for name, category := range result.Audit {
		fmt.Printf("  %-14s %5.1f\n", name, category.Score)
	}
	for _, p := range result.Priorities {
		fmt.Printf("  [%s] %s: %s\n", p.Severity, p.Issue, p.Fix)
	}
}

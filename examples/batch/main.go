// Audit every URL listed on stdin (one per line), 10 per request, and print a
// CSV of url,score,grade. Batch audits need a paid plan.
//
//	SEOSCORE_API_KEY=ssa_... go run ./examples/batch < urls.txt
package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strings"

	seoscoreapi "github.com/SeoScoreAPI/go-client"
)

func main() {
	var urls []string
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			urls = append(urls, line)
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	client := seoscoreapi.New()
	results, err := client.AuditAll(context.Background(), urls, nil)
	out := csv.NewWriter(os.Stdout)
	_ = out.Write([]string{"url", "score", "grade", "error"})
	for _, r := range results {
		_ = out.Write([]string{r.URL, fmt.Sprintf("%.1f", r.Score), r.Grade, r.Error})
	}
	out.Flush()
	if err != nil {
		log.Fatalf("stopped after %d of %d URLs: %v", len(results), len(urls), err)
	}
}

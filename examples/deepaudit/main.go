// Run a Deep Site Audit and wait for the result, printing progress as it goes.
// Deep Site Audits need the Pro or Ultra plan, or Deep Audit credits.
//
//	SEOSCORE_API_KEY=ssa_... go run ./examples/deepaudit https://example.com
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	seoscoreapi "github.com/SeoScoreAPI/go-client"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: deepaudit <url>")
	}
	client := seoscoreapi.New()

	// Stop waiting on Ctrl-C or after 20 minutes. The audit keeps running on the
	// server; GetDeepAudit can pick it up later with its job id.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()

	result, err := client.DeepAudit(ctx, os.Args[1],
		&seoscoreapi.DeepAuditOptions{BusinessType: "saas"},
		&seoscoreapi.WaitOptions{OnProgress: func(s any) {
			st := s.(*seoscoreapi.DeepAuditStatus)
			switch st.Status {
			case seoscoreapi.StatusQueued:
				fmt.Printf("queued (position %d)\n", st.QueuePosition)
			case seoscoreapi.StatusRunning:
				fmt.Printf("running %3.0f%% %s\n", st.Progress, st.Stage)
			}
		}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("score %v (%v)\n", result.Scores["lai_score"], result.Scores["lai_grade"])
}

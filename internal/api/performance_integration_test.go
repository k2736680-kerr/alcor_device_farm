package api_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestManagementQueryAtTwentyRPSKeepsP95BelowThreeHundredMilliseconds(t *testing.T) {
	environment := newManagementEnvironment(t)
	const requests = 100
	const maximumConcurrency = 20
	durations := make(chan time.Duration, requests)
	errorsChannel := make(chan error, requests)
	semaphore := make(chan struct{}, maximumConcurrency)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	var waitGroup sync.WaitGroup

	for index := 0; index < requests; index++ {
		<-ticker.C
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, environment.server.URL+"/api/v1/devices", nil)
			if err != nil {
				errorsChannel <- err
				return
			}
			request.Header.Set("Authorization", "Bearer "+serviceToken)
			startedAt := time.Now()
			response, err := environment.server.Client().Do(request)
			duration := time.Since(startedAt)
			if err != nil {
				errorsChannel <- err
				return
			}
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode != http.StatusOK {
				errorsChannel <- fmt.Errorf("query status=%d", response.StatusCode)
				return
			}
			durations <- duration
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}
	close(durations)
	values := make([]time.Duration, 0, requests)
	for duration := range durations {
		values = append(values, duration)
	}
	if len(values) != requests {
		t.Fatalf("completed queries=%d want=%d", len(values), requests)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	p95 := values[(len(values)*95+99)/100-1]
	t.Logf("20 RPS query sample=%d p95=%s", len(values), p95)
	if p95 > 300*time.Millisecond {
		t.Fatalf("query p95=%s exceeds 300ms", p95)
	}
}

package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/Ad-Quanta/alcor-device-farm/adapters/alcor/mockserver"
)

func main() {
	address := flag.String("address", "127.0.0.1:18080", "mock server listen address")
	scenario := flag.String("scenario", mockserver.ScenarioHappy, "happy, capacity_unavailable, or infra_failure")
	pendingPolls := flag.Int("pending-polls", 1, "GET polls before a pending reservation becomes active")
	flag.Parse()
	token := os.Getenv("DEVICE_FARM_TOKEN")
	if token == "" {
		token = "mock-service-token"
	}

	server := &http.Server{Addr: *address, Handler: mockserver.New(mockserver.Config{
		Token: token, Scenario: *scenario, PendingPolls: *pendingPolls,
	})}
	log.Printf("Alcor Device Farm adapter mock listening on %s scenario=%s", *address, *scenario)
	log.Fatal(server.ListenAndServe())
}

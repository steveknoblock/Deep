// Command deep runs the Deep application server: an orchestration layer
// implementing the Notebook application on top of Hatcheck's generic
// primitives. Deep is a plain HTTP client of Hatcheck — no different in
// principle from any other application that might be built against it —
// and holds no credentials or data of its own. Every call it makes to
// Hatcheck carries the calling browser's own session and capability,
// obtained by logging into Hatcheck directly; identity and access control
// stay entirely Hatcheck's concern.
package main

import (
	"log"
	"net/http"

	"github.com/steveknoblock/Deep/internal/hatcheckclient"
	"github.com/steveknoblock/Deep/internal/notebook"
)

func main() {
	cfg := LoadConfig()

	hc := hatcheckclient.New(cfg.HatcheckURL)
	svc := notebook.NewService(hc)

	mux := http.NewServeMux()
	registerRoutes(mux, svc, cfg)

	log.Printf("Deep listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatal(err)
	}
}

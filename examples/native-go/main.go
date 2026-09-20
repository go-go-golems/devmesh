// Command native-go demonstrates a Go service that binds an ephemeral backend
// port and publishes it under a stable devmesh name.
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	devmesh "github.com/wesen/devmesh/pkg/devmesh"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ln, err := devmesh.ListenTCP(ctx, "example.api", 8080)
	if err != nil {
		log.Fatalf("devmesh registration failed: %v", err)
	}
	defer ln.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello from %s via %s\n", ln.Addr(), ln.Registration.Endpoint())
	})

	log.Printf("backend: %s", ln.Addr())
	log.Printf("stable frontend: %s", ln.Registration.Endpoint())
	if err := http.Serve(ln, mux); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// TestPostgresThroughFrontend is the end-to-end acceptance test: a labeled
// PostgreSQL container is discovered, and a real PostgreSQL client connects
// through the stable devmesh frontend.
func TestPostgresThroughFrontend(t *testing.T) {
	dapi := dockerAPI(t)
	if !hasImage(t, dapi, testImage) {
		t.Skipf("image %s not available locally", testImage)
	}

	h := startHarnessWithDocker(t, 5*time.Second, true)
	const name = "checkout.pgx"

	createPostgres(t, dapi, "devmesh-pgx", "db", name)
	svc := waitReady(t, h, name, 45*time.Second)

	dsn := fmt.Sprintf("postgres://dev:dev@%s:%d/app?sslmode=disable",
		svc.Frontend.Host, svc.Frontend.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	var conn *pgx.Conn
	var err error
	deadline := time.Now().Add(75 * time.Second)
	for time.Now().Before(deadline) {
		conn, err = pgx.Connect(ctx, dsn)
		if err == nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("pgx connect through %s: %v", dsn, err)
	}
	defer conn.Close(context.Background())

	var one int
	if err := conn.QueryRow(ctx, "select 1").Scan(&one); err != nil {
		t.Fatalf("select 1 through devmesh: %v", err)
	}
	if one != 1 {
		t.Fatalf("select 1 = %d", one)
	}
}

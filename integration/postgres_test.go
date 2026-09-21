package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/jackc/pgx/v5"

	"github.com/go-go-golems/devmesh/internal/api"
)

// TestPostgresThroughFrontend is the end-to-end acceptance test: a labeled
// PostgreSQL container is discovered, a real PostgreSQL client queries through
// the stable frontend, then a replacement container is queried through that
// same frontend after its Docker backend changes.
func TestPostgresThroughFrontend(t *testing.T) {
	dapi := dockerAPI(t)
	if !hasImage(t, dapi, testImage) {
		t.Skipf("image %s not available locally", testImage)
	}

	h := startHarnessWithDocker(t, 5*time.Second, true)
	const name = "checkout.pgx"

	firstID := createPostgres(t, dapi, "devmesh-pgx", "db", name)
	svc := waitReady(t, h, name, 45*time.Second)
	dsn := fmt.Sprintf("postgres://dev:dev@%s:%d/app?sslmode=disable", svc.Frontend.Host, svc.Frontend.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	queryOne(t, ctx, dsn)

	// Start B before removing A to exercise the stale-old-container removal
	// path, then prove a new PostgreSQL connection works through the unchanged
	// frontend after A disappears.
	secondID := createPostgres(t, dapi, "devmesh-pgx", "db", name)
	deadline := time.Now().Add(45 * time.Second)
	var inspect api.InspectDTO
	for time.Now().Before(deadline) {
		if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name+"/inspect", nil, &inspect); err == nil && inspect.Backend != nil && inspect.DockerContainerID == secondID {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if inspect.Backend == nil || inspect.DockerContainerID != secondID {
		t.Fatalf("replacement not current: %+v", inspect)
	}

	rmctx, rmcancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := dapi.ContainerRemove(rmctx, firstID, container.RemoveOptions{Force: true, RemoveVolumes: true}); err != nil {
		rmcancel()
		t.Fatalf("remove first postgres: %v", err)
	}
	rmcancel()
	time.Sleep(500 * time.Millisecond)
	var after api.InspectDTO
	if err := h.client.Do(context.Background(), "GET", "/v1/services/"+name+"/inspect", nil, &after); err != nil {
		t.Fatal(err)
	}
	if after.Status != "ready" || after.DockerContainerID != secondID || after.Frontend.Port != svc.Frontend.Port {
		t.Fatalf("replacement displaced after first removal: %+v", after)
	}
	queryOne(t, ctx, dsn)
}

func queryOne(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
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
	defer func() { _ = conn.Close(context.Background()) }()
	var one int
	if err := conn.QueryRow(ctx, "select 1").Scan(&one); err != nil {
		t.Fatalf("select 1 through devmesh: %v", err)
	}
	if one != 1 {
		t.Fatalf("select 1 = %d", one)
	}
}

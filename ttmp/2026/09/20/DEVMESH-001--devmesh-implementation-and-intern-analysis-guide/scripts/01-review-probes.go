// Review verification probes, updated for the v2 hardening implementation.
// No production files modified. Run from repository root:
//
//	go run ./ttmp/2026/09/20/DEVMESH-001--devmesh-implementation-and-intern-analysis-guide/scripts/01-review-probes.go
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/wesen/devmesh/internal/config"
	"github.com/wesen/devmesh/internal/daemon"
	"github.com/wesen/devmesh/internal/dockerwatch"
	"github.com/wesen/devmesh/internal/lease"
	"github.com/wesen/devmesh/internal/proxy"
	"github.com/wesen/devmesh/internal/registry"
	"github.com/wesen/devmesh/internal/runtime"
	"github.com/wesen/devmesh/internal/state"
	"github.com/wesen/devmesh/internal/transport"
)

var logger = slog.New(slog.NewTextHandler(io.Discard, nil))

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func newDaemon(dir string) *daemon.Daemon {
	c := config.Default()
	c.Docker.Enabled = false
	c.StatePath = filepath.Join(dir, "state.json")
	d, e := daemon.New(c, logger)
	must(e)
	return d
}
func reg(d *daemon.Daemon, name, owner string, port int) daemon.RegisterResult {
	r, e := d.Register(daemon.RegisterParams{Name: name, OwnerKey: owner, Backend: registry.Backend{Host: "127.0.0.1", Port: port}})
	must(e)
	return r
}
func insp(id string) container.InspectResponse {
	return container.InspectResponse{ContainerJSONBase: &container.ContainerJSONBase{ID: id, Name: "/review-db", State: &container.State{Running: true}}, Config: &container.Config{Labels: map[string]string{dockerwatch.LabelEnable: "true", dockerwatch.LabelName: "docker.svc", dockerwatch.LabelContainerPort: "5432", dockerwatch.ComposeProjectLabel: "review", dockerwatch.ComposeServiceLabel: "db"}}, NetworkSettings: &container.NetworkSettings{NetworkSettingsBase: container.NetworkSettingsBase{Ports: nat.PortMap{"5432/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "49173"}}}}}}
}

type fakeDocker struct {
	dockerwatch.DockerAPI
	current container.InspectResponse
}

func (f *fakeDocker) ContainerList(context.Context, container.ListOptions) ([]container.Summary, error) {
	return []container.Summary{{ID: f.current.ID, Labels: f.current.Config.Labels}}, nil
}
func (f *fakeDocker) ContainerInspect(context.Context, string) (container.InspectResponse, error) {
	return f.current, nil
}

func main() {
	dir, e := os.MkdirTemp("", "devmesh-review-")
	must(e)
	defer os.RemoveAll(dir)
	d := newDaemon(dir)
	defer d.Shutdown(context.Background())
	old := reg(d, "lease.svc", "manual:same", 49171)
	reg(d, "lease.svc", "manual:same", 49172)
	deleteErr := d.DeleteRegistration(old.RegistrationID, old.LeaseToken)
	rec, _ := d.Registry.Resolve("lease.svc")
	fmt.Printf("P01 old lease DELETE after replacement: status=%s stale_delete_rejected=%v (desired ready/true)\n", rec.Status, deleteErr != nil)
	reg(d, "foreign.svc", "manual:current", 49172)
	d.ForgetDockerPublication("foreign.svc", "docker:old", "old-container")
	rec, _ = d.Registry.Resolve("foreign.svc")
	rt, _ := d.Runtime.Get("foreign.svc")
	fmt.Printf("P02 stale foreign forget: registry=%s runtime_backend_nil=%v (desired false)\n", rec.Status, rt.CurrentBackend() == nil)
	f := &fakeDocker{current: insp("old")}
	w := dockerwatch.NewWatcher(f, false, dockerwatch.Callbacks{OnRegister: func(_ context.Context, r dockerwatch.Registration) error {
		_, e := d.Register(daemon.RegisterParams{Name: r.Name, OwnerKey: r.OwnerKey, Source: registry.SourceDocker, DockerContainerID: r.ContainerID, Backend: registry.Backend{Host: r.BackendHost, Port: r.BackendPort}})
		return e
	}, OnForget: func(r dockerwatch.Registration) {
		d.ForgetDockerPublication(r.Name, r.OwnerKey, r.ContainerID)
	}}, logger)
	must(w.Reconcile(context.Background()))
	f.current = insp("new")
	must(w.Reconcile(context.Background()))
	rec, _ = d.Registry.Resolve("docker.svc")
	fmt.Printf("P03 Docker reconcile replacement: container=%s status=%s (desired ready)\n", rec.DockerContainerID, rec.Status)
	_, e = d.Register(daemon.RegisterParams{Name: "http.a", Kind: registry.KindHTTP, Source: registry.SourceManual, OwnerKey: "http.a", HTTPHost: "same.test", Backend: registry.Backend{Host: "127.0.0.1", Port: 49200}})
	must(e)
	_, duplicateErr := d.Register(daemon.RegisterParams{Name: "http.b", Kind: registry.KindHTTP, Source: registry.SourceManual, OwnerKey: "http.b", HTTPHost: "same.test", Backend: registry.Backend{Host: "127.0.0.1", Port: 49201}})
	provider, _ := d.HTTP.Lookup("same.test")
	fmt.Printf("P04 duplicate HTTP host rejected=%v backend_port=%d (desired true/49200)\n", duplicateErr != nil, provider().Port)
	_, mutationErr := d.Register(daemon.RegisterParams{Name: "http.a", Kind: registry.KindHTTP, Source: registry.SourceManual, OwnerKey: "http.a", HTTPHost: "new.test", Backend: registry.Backend{Host: "127.0.0.1", Port: 49202}})
	rec, _ = d.Registry.Resolve("http.a")
	fmt.Printf("P05 HTTP host mutation rejected=%v hostname=%s advertised_url=%s (desired true/same.test/configured port)\n", mutationErr != nil, rec.Hostname, rec.Frontend.URL)
	m := lease.NewManager(15 * time.Second)
	entry := m.AddWithTTL("ttl", "ttl.svc", "o", "t", 60*time.Second)
	exp, e := m.Renew("ttl", "t")
	must(e)
	fmt.Printf("P06 requested TTL=60s: initial~%.0fs renewed~%.0fs (desired 60s)\n", time.Until(entry.ExpiresAt).Seconds(), time.Until(exp).Seconds())
	m2 := lease.NewManager(time.Minute)
	m2.Add("id", "svc", "owner", "token")
	taken := m2.TakeExpired(time.Now().Add(2 * time.Minute))
	_, renewErr := m2.Renew("id", "token")
	fmt.Printf("P07 atomic expiry take: taken=%d renew_rejected=%v leases=%d (desired 1/true/0)\n", len(taken), renewErr != nil, m2.Len())
	mixed := insp("mixed")
	mixed.NetworkSettings.Ports["5432/tcp"] = append(mixed.NetworkSettings.Ports["5432/tcp"], nat.PortBinding{HostIP: "0.0.0.0", HostPort: "49174"})
	_, e = dockerwatch.RegistrationFromInspect(mixed, false)
	fmt.Printf("P08 mixed loopback+wildcard publication accepted=%v (desired refusal)\n", e == nil)
	sock := filepath.Join(dir, "not-a-socket")
	must(os.WriteFile(sock, []byte("valuable file"), 0600))
	ln, e := transport.Listen(sock)
	must(e)
	st, e := os.Stat(sock)
	must(e)
	fmt.Printf("P09 preexisting regular file replaced with socket=%v (desired refusal)\n", st.Mode()&os.ModeSocket != 0)
	ln.Close()
	safe := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "SAFE") }))
	defer safe.Close()
	unintended := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "UNINTENDED") }))
	defer unintended.Close()
	host, rawPort, err := net.SplitHostPort(strings.TrimPrefix(safe.URL, "http://"))
	must(err)
	port, err := strconv.Atoi(rawPort)
	must(err)
	router := proxy.NewRouter("http", 80, logger)
	calls := 0
	router.Set("route.test", func() *registry.Backend {
		calls++
		if calls == 1 {
			return &registry.Backend{Host: host, Port: port}
		}
		return nil
	})
	req := httptest.NewRequest("GET", unintended.URL+"/", nil)
	req.Host = "route.test"
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	fmt.Printf("P10 one backend snapshot: status=%d body=%s provider_calls=%d (desired 200/SAFE/1)\n", rr.Code, rr.Body.String(), calls)
	badPath := filepath.Join(dir, "parent", "state.json")
	store, e := state.Load(badPath)
	must(e)
	must(os.WriteFile(filepath.Join(dir, "parent"), []byte("not a dir"), 0600))
	alloc, e := runtime.NewAllocator("127.0.0.1", 31000, 31100, store, logger).Allocate("durability", 0)
	must(e)
	defer alloc.Listener.Close()
	_, diskErr := os.Stat(badPath)
	retry := store.SetPort("durability", alloc.Port)
	fmt.Printf("P11 allocation after persistence failure: success=true disk_missing=%v same_value_retry_error=%v\n", diskErr != nil, retry)
	backend, e := net.Listen("tcp", "127.0.0.1:0")
	must(e)
	defer backend.Close()
	go func() {
		c, e := backend.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		io.Copy(c, c)
	}()
	addr := backend.Addr().(*net.TCPAddr)
	res := reg(d, "shutdown.svc", "manual:shutdown", addr.Port)
	conn, e := net.Dial("tcp", res.Frontend.Addr())
	must(e)
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, e = conn.Write([]byte("a"))
	must(e)
	buf := make([]byte, 1)
	_, e = io.ReadFull(conn, buf)
	must(e)
	must(d.Shutdown(context.Background()))
	_, e = conn.Write([]byte("b"))
	must(e)
	_, e = io.ReadFull(conn, buf)
	fmt.Printf("P12 established TCP after daemon Shutdown: read_error=%v payload=%s (desired closed after drain deadline)\n", e, string(buf))
}

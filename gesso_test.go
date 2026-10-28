package gesso_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	gessoplugin "github.com/Elagoht/collage-gesso"
	"github.com/Elagoht/collage/pkg/collage"
	"github.com/Elagoht/gesso"
)

// trail records what happened, in order, from any goroutine.
type trail struct {
	mu    sync.Mutex
	steps []string
}

func (tr *trail) add(s string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.steps = append(tr.steps, s)
}

func (tr *trail) get() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.steps)
}

type slowIn struct{}

type slowOut struct {
	OK bool `json:"ok"`
}

var slow = gesso.Endpoint[slowIn, slowOut]{Method: "GET", Pattern: "/slow"}

// worker is a gesso module with a lifecycle and a route that waits for release.
type worker struct {
	tr       *trail
	startErr error
	entered  chan struct{}
	release  chan struct{}
}

func (w *worker) Routes(r *gesso.Router) {
	slow.Handle(r, func(context.Context, slowIn) (slowOut, error) {
		w.tr.add("request")
		close(w.entered)
		<-w.release
		w.tr.add("answered")
		return slowOut{OK: true}, nil
	})
}

func (w *worker) Start(context.Context) error {
	w.tr.add("start")
	return w.startErr
}

func (w *worker) Stop(context.Context) error {
	w.tr.add("stop")
	return nil
}

func newWorker(tr *trail) *worker {
	return &worker{tr: tr, entered: make(chan struct{}), release: make(chan struct{})}
}

type logs struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logs) Enabled(context.Context, slog.Level) bool { return true }
func (l *logs) WithAttrs([]slog.Attr) slog.Handler       { return l }
func (l *logs) WithGroup(string) slog.Handler            { return l }
func (l *logs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	line := r.Message
	r.Attrs(func(a slog.Attr) bool { line += " " + a.String(); return true })
	l.msgs = append(l.msgs, line)
	return nil
}

func (l *logs) has(part string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.msgs, func(m string) bool { return strings.Contains(m, part) })
}

func newAPI(t *testing.T, w *worker, tr *trail) *gesso.App {
	t.Helper()
	api, err := gesso.New(gesso.Config{BasePath: "/api", Logger: slog.New(&logs{})}, w)
	if err != nil {
		t.Fatal(err)
	}
	api.OnShutdown(func(context.Context) error { tr.add("close db"); return nil })
	return api
}

func newApp(t *testing.T, api *gesso.App, server collage.ServerConfig, l *logs) *collage.App {
	t.Helper()
	a, err := collage.New(&collage.Config{
		Server: server,
		Logger: slog.New(l),
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/p.html": {Data: []byte(`<main>home</main>`)},
		}, Root: "t"},
		Plugins: []collage.Plugin{gessoplugin.New(gessoplugin.Options{App: api})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RegisterPage(collage.NewPage("home").WithContent(collage.NewFragment("home", "p.html").Build()).WithPath("en", "/").Build()); err != nil {
		t.Fatal(err)
	}
	if err := a.Handle("/api/", api); err != nil {
		t.Fatal(err)
	}
	return a
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return port
}

func eventually(t *testing.T, cond func() bool, what string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

func serve(t *testing.T, a *collage.App, port int) chan error {
	served := make(chan error, 1)
	go func() { served <- a.ListenAndServe() }()
	eventually(t, func() bool {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			return false
		}
		res.Body.Close()
		return true
	}, "the server to answer")
	return served
}

// The app starts when collage serves, and stops only after the request in
// flight has been answered: collage stops its server before it shuts plugins
// down. The app's OnShutdown hooks run last.
func TestServeStartsAndShutdownWaitsForRequests(t *testing.T) {
	tr := &trail{}
	w := newWorker(tr)
	port := freePort(t)
	a := newApp(t, newAPI(t, w, tr), collage.ServerConfig{Host: "127.0.0.1", Port: port, ShutdownTimeout: 5 * time.Second}, &logs{})
	served := serve(t, a, port)

	remote := gesso.NewRemote(fmt.Sprintf("http://127.0.0.1:%d/api", port), nil)
	answer := make(chan error, 1)
	go func() {
		out, err := slow.Call(context.Background(), remote, slowIn{})
		if err == nil && !out.OK {
			err = errors.New("not ok")
		}
		answer <- err
	}()
	<-w.entered

	shut := make(chan error, 1)
	go func() { shut <- a.Shutdown(context.Background()) }()
	time.Sleep(100 * time.Millisecond) // a plugin shut down too early would have stopped by now
	if got := tr.get(); slices.Contains(got, "stop") {
		t.Fatalf("the app stopped with a request in flight: %v", got)
	}
	close(w.release)
	if err := <-answer; err != nil {
		t.Fatalf("the request in flight: %v", err)
	}
	if err := <-shut; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-served; err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	if want := []string{"start", "request", "answered", "stop", "close db"}; !slices.Equal(tr.get(), want) {
		t.Fatalf("got %v, want %v", tr.get(), want)
	}
}

// A static build initialises the plugins but must not start the app's modules: a
// queue worker would handle jobs while the site is being written to disk.
func TestBuildStartsNothing(t *testing.T) {
	tr := &trail{}
	a := newApp(t, newAPI(t, newWorker(tr), tr), collage.ServerConfig{Host: "localhost", Port: 3000}, &logs{})
	if _, err := collage.Build(context.Background(), a, collage.BuildOptions{OutDir: t.TempDir()}); err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := tr.get(); slices.Contains(got, "start") {
		t.Fatalf("the build started the app: %v", got)
	}
}

// An app whose modules failed to start answers 503, and the failure is logged.
func TestFailedStartIsLoggedAndAnswered503(t *testing.T) {
	tr := &trail{}
	w := newWorker(tr)
	w.startErr = errors.New("no redis")
	l := &logs{}
	port := freePort(t)
	a := newApp(t, newAPI(t, w, tr), collage.ServerConfig{Host: "127.0.0.1", Port: port}, l)
	served := serve(t, a, port)
	t.Cleanup(func() {
		if err := a.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
		<-served
	})

	_, err := slow.Call(context.Background(), gesso.NewRemote(fmt.Sprintf("http://127.0.0.1:%d/api", port), nil), slowIn{})
	if !errors.Is(err, gesso.ErrUnavailable) {
		t.Fatalf("got %v, want ErrUnavailable", err)
	}
	if !l.has("no redis") {
		t.Fatalf("the failure was not logged: %v", l.msgs)
	}
}

func TestInitWithoutAnAppFails(t *testing.T) {
	a, err := collage.New(&collage.Config{
		Logger: slog.New(&logs{}),
		Template: collage.TemplateConfig{FS: fstest.MapFS{
			"t/p.html": {Data: []byte(`<main>home</main>`)},
		}, Root: "t"},
		Plugins: []collage.Plugin{gessoplugin.New(gessoplugin.Options{})},
	})
	if err == nil {
		err = a.Start()
	}
	if err == nil || !strings.Contains(err.Error(), "Options.App is nil") {
		t.Fatalf("got %v", err)
	}
}

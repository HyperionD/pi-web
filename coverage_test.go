package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/Epsilondelta-ai/pi-web/internal/piweb"
)

func TestMainSuccessAndError(t *testing.T) {
	previousCommand := newRootCommandForMain
	previousDeps := defaultRootDependenciesForMain
	previousStderr := stderrForMain
	previousExit := exitForMain
	t.Cleanup(func() {
		newRootCommandForMain = previousCommand
		defaultRootDependenciesForMain = previousDeps
		stderrForMain = previousStderr
		exitForMain = previousExit
	})

	defaultRootDependenciesForMain = func() rootDependencies { return rootDependencies{stdout: io.Discard} }
	newRootCommandForMain = func(rootDependencies) rootCommandExecutor {
		return commandFunc(func() error { return nil })
	}
	main()

	var stderr bytes.Buffer
	var exitCode int
	stderrForMain = &stderr
	exitForMain = func(code int) { exitCode = code }
	newRootCommandForMain = func(rootDependencies) rootCommandExecutor {
		return commandFunc(func() error { return errors.New("boom") })
	}
	main()
	if exitCode != 1 || !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("expected exit and stderr, code=%d stderr=%q", exitCode, stderr.String())
	}
}

type commandFunc func() error

func (f commandFunc) Execute() error { return f() }

func TestDefaultRootDependenciesAndStaticFiles(t *testing.T) {
	deps := defaultRootDependencies()
	if deps.stdout == nil || deps.stderr == nil || deps.serve == nil {
		t.Fatalf("incomplete dependencies: %+v", deps)
	}
	if _, err := fs.Stat(staticFiles(), "README.txt"); err != nil {
		t.Fatalf("static files missing README: %v", err)
	}
	if _, err := fs.Stat(mustSub(fstest.MapFS{"dir/file.txt": {Data: []byte("ok")}}, "dir"), "file.txt"); err != nil {
		t.Fatalf("mustSub success: %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("expected mustSub panic")
		}
	}()
	_ = mustSub(fstest.MapFS{}, "../missing")
}

func TestRunServerWithDependencies(t *testing.T) {
	base := func() serverDependencies {
		return serverDependencies{
			newAutoStore: piweb.NewMockStore,
			newMockStore: piweb.NewMockStore,
			newServer:    piweb.NewServer,
			newBroker:    piweb.NewBroker,
			staticFiles:  func() fs.FS { return fstest.MapFS{"index.html": {Data: []byte("ok")}} },
			listen:       func(*http.Server) error { return http.ErrServerClosed },
			shutdown:     func(*http.Server, context.Context) error { return nil },
			notify:       func(chan<- os.Signal, ...os.Signal) {},
			stopNotify:   func(chan<- os.Signal) {},
		}
	}

	if err := runServerWithDependencies(serverOptions{Host: "127.0.0.1", Port: "0"}, base()); err != nil {
		t.Fatalf("closed server should return nil: %v", err)
	}

	listenErr := errors.New("listen failed")
	deps := base()
	deps.listen = func(*http.Server) error { return listenErr }
	if err := runServerWithDependencies(serverOptions{}, deps); !errors.Is(err, listenErr) {
		t.Fatalf("expected listen error, got %v", err)
	}

	shutdownErr := errors.New("shutdown failed")
	deps = base()
	block := make(chan struct{})
	deps.listen = func(*http.Server) error {
		<-block
		return http.ErrServerClosed
	}
	deps.notify = func(stop chan<- os.Signal, _ ...os.Signal) { stop <- os.Interrupt }
	deps.shutdown = func(*http.Server, context.Context) error {
		close(block)
		return shutdownErr
	}
	if err := runServerWithDependencies(serverOptions{Mock: true}, deps); !errors.Is(err, shutdownErr) {
		t.Fatalf("expected shutdown error, got %v", err)
	}

	deps = base()
	block = make(chan struct{})
	deps.listen = func(*http.Server) error {
		<-block
		return http.ErrServerClosed
	}
	deps.notify = func(stop chan<- os.Signal, _ ...os.Signal) { stop <- os.Interrupt }
	deps.shutdown = func(*http.Server, context.Context) error {
		close(block)
		return nil
	}
	if err := runServerWithDependencies(serverOptions{Mock: true}, deps); err != nil {
		t.Fatalf("expected clean shutdown, got %v", err)
	}
}

func TestRunServerWithOpenServer(t *testing.T) {
	base := func() serverDependencies {
		return serverDependencies{
			newAutoStore: piweb.NewMockStore,
			newMockStore: piweb.NewMockStore,
			newServer:    piweb.NewServer,
			newBroker:    piweb.NewBroker,
			staticFiles:  func() fs.FS { return fstest.MapFS{"index.html": {Data: []byte("ok")}} },
			listen:       func(*http.Server) error { return http.ErrServerClosed },
			shutdown:     func(*http.Server, context.Context) error { return nil },
			notify:       func(chan<- os.Signal, ...os.Signal) {},
			stopNotify:   func(chan<- os.Signal) {},
		}
	}

	dbPath := filepath.Join(t.TempDir(), "web.db")
	dbStore := piweb.NewWebStore(dbPath)

	deps := base()
	deps.newAutoStore = func() *piweb.Store { return dbStore }
	deps.openServer = func(piweb.Config, *piweb.Store, *piweb.Broker) (*piweb.Server, error) {
		return piweb.NewServer(piweb.Config{}, piweb.NewMockStore(), piweb.NewBroker()), nil
	}
	if err := runServerWithDependencies(serverOptions{}, deps); err != nil {
		t.Fatalf("openServer success path: %v", err)
	}

	openErr := errors.New("open failed")
	deps = base()
	deps.newAutoStore = func() *piweb.Store { return piweb.NewWebStore(dbPath) }
	deps.openServer = func(piweb.Config, *piweb.Store, *piweb.Broker) (*piweb.Server, error) {
		return nil, openErr
	}
	if err := runServerWithDependencies(serverOptions{}, deps); !errors.Is(err, openErr) {
		t.Fatalf("expected open error, got %v", err)
	}

	mockDeps := base()
	mockCalls := 0
	mockDeps.openServer = func(piweb.Config, *piweb.Store, *piweb.Broker) (*piweb.Server, error) {
		mockCalls++
		return nil, errors.New("should not be called for mock stores")
	}
	if err := runServerWithDependencies(serverOptions{Mock: true}, mockDeps); err != nil {
		t.Fatalf("mock fallback path: %v", err)
	}
	if mockCalls != 0 {
		t.Fatalf("mock store unexpectedly used the durable server: %d", mockCalls)
	}
}

func TestDefaultServerDependencies(t *testing.T) {
	deps := defaultServerDependencies()
	if deps.newAutoStore == nil || deps.newMockStore == nil || deps.newServer == nil || deps.openServer == nil ||
		deps.newBroker == nil ||
		deps.staticFiles == nil || deps.listen == nil || deps.shutdown == nil ||
		deps.notify == nil || deps.stopNotify == nil {
		t.Fatalf("incomplete server dependencies: %+v", deps)
	}
}

func TestRunServerWrapperUsesDefaults(t *testing.T) {
	previous := defaultServerDependenciesForRun
	t.Cleanup(func() { defaultServerDependenciesForRun = previous })
	deps := serverDependencies{
		newAutoStore: piweb.NewMockStore,
		newMockStore: piweb.NewMockStore,
		newServer:    piweb.NewServer,
		newBroker:    piweb.NewBroker,
		staticFiles:  func() fs.FS { return fstest.MapFS{"index.html": {Data: []byte("ok")}} },
		listen:       func(*http.Server) error { return http.ErrServerClosed },
		shutdown:     func(*http.Server, context.Context) error { return nil },
		notify:       func(chan<- os.Signal, ...os.Signal) {},
		stopNotify:   func(chan<- os.Signal) {},
	}
	defaultServerDependenciesForRun = func() serverDependencies { return deps }
	if err := runServer(serverOptions{}); err != nil {
		t.Fatalf("runServer wrapper: %v", err)
	}
}

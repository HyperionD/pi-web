package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Epsilondelta-ai/pi-web/internal/piweb"
)

type serverDependencies struct {
	newAutoStore func() *piweb.Store
	newMockStore func() *piweb.Store
	newServer    func(piweb.Config, *piweb.Store, *piweb.Broker) *piweb.Server
	openServer   func(piweb.Config, *piweb.Store, *piweb.Broker) (*piweb.Server, error)
	newBroker    func() *piweb.Broker
	staticFiles  func() fs.FS
	listen       func(*http.Server) error
	shutdown     func(*http.Server, context.Context) error
	notify       func(chan<- os.Signal, ...os.Signal)
	stopNotify   func(chan<- os.Signal)
}

func defaultServerDependencies() serverDependencies {
	return serverDependencies{
		newAutoStore: piweb.NewAutoStore,
		newMockStore: piweb.NewMockStore,
		newServer:    piweb.NewServer,
		openServer:   piweb.OpenServer,
		newBroker:    piweb.NewBroker,
		staticFiles:  staticFiles,
		listen:       (*http.Server).ListenAndServe,
		shutdown:     (*http.Server).Shutdown,
		notify:       signal.Notify,
		stopNotify:   signal.Stop,
	}
}

var defaultServerDependenciesForRun = defaultServerDependencies

func runServer(options serverOptions) error {
	return runServerWithDependencies(options, defaultServerDependenciesForRun())
}

func runServerWithDependencies(options serverOptions, deps serverDependencies) error {
	if err := piweb.PrepareUserManagementDB(); err != nil {
		return fmt.Errorf("prepare user database: %w", err)
	}
	store := deps.newAutoStore()
	if options.Mock {
		store = deps.newMockStore()
	}

	serverConfig := piweb.Config{
		Host:              options.Host,
		Port:              options.Port,
		EnablePiExecution: !options.Mock,
		UserManagement:    true,
		StaticFiles:       deps.staticFiles(),
	}
	var server *piweb.Server
	if deps.openServer != nil && store.DatabasePath() != "" {
		opened, err := deps.openServer(serverConfig, store, deps.newBroker())
		if err != nil {
			return fmt.Errorf("open server: %w", err)
		}
		server = opened
	} else {
		server = deps.newServer(serverConfig, store, deps.newBroker())
	}
	httpServer := &http.Server{
		Addr:              server.Addr(),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("pi web backend listening", "addr", httpServer.Addr)
		if err := deps.listen(httpServer); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}()

	stop := make(chan os.Signal, 1)
	deps.notify(stop, os.Interrupt, syscall.SIGTERM)
	defer deps.stopNotify(stop)

	select {
	case err := <-serverErrors:
		if err != nil {
			slog.Error("server failed", "error", err)
			return err
		}
		return nil
	case <-stop:
	}

	serverCtx, serverCancel := context.WithTimeout(context.Background(), 5*time.Second)
	serverShutdownErr := server.Shutdown(serverCtx)
	serverCancel()
	if serverShutdownErr != nil {
		slog.Error("runtime shutdown failed", "error", serverShutdownErr)
	}
	httpCtx, httpCancel := context.WithTimeout(context.Background(), 5*time.Second)
	httpShutdownErr := deps.shutdown(httpServer, httpCtx)
	httpCancel()
	if httpShutdownErr != nil {
		slog.Error("shutdown failed", "error", httpShutdownErr)
	}
	listenErr := <-serverErrors
	if listenErr != nil {
		slog.Error("listen goroutine failed during shutdown", "error", listenErr)
	}
	if err := errors.Join(serverShutdownErr, httpShutdownErr, listenErr); err != nil {
		return err
	}
	slog.Info("pi web backend stopped")
	return nil
}

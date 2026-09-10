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

	server := deps.newServer(piweb.Config{
		Host:              options.Host,
		Port:              options.Port,
		EnablePiExecution: !options.Mock,
		UserManagement:    true,
		StaticFiles:       deps.staticFiles(),
	}, store, deps.newBroker())
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverShutdownErr := server.Shutdown(ctx)
	if serverShutdownErr != nil {
		slog.Error("runtime shutdown failed", "error", serverShutdownErr)
	}
	httpShutdownErr := deps.shutdown(httpServer, ctx)
	if httpShutdownErr != nil {
		slog.Error("shutdown failed", "error", httpShutdownErr)
	}
	if err := errors.Join(serverShutdownErr, httpShutdownErr); err != nil {
		return err
	}
	slog.Info("pi web backend stopped")
	return nil
}

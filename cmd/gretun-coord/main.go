// Command gretun-coord runs a gretun coordinator: peer registry + signaling
// relay over HTTP(S). It never sees tunnel payload and never holds any
// peer's disco private key, so compromise only exposes public metadata.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/HueCodes/gretun/internal/coord"
)

func main() {
	listen := flag.String("listen", ":8443", "listen address")
	poolStr := flag.String("pool", "100.64.0.0/24", "CIDR from which to assign tunnel IPs")
	certFile := flag.String("cert", "", "TLS cert file (enables HTTPS)")
	keyFile := flag.String("key", "", "TLS key file (enables HTTPS)")
	verbose := flag.Bool("v", false, "debug logging")
	flag.Parse()

	if *verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
	}

	pool, err := netip.ParsePrefix(*poolStr)
	if err != nil {
		fatal("invalid --pool: %v", err)
	}

	store := coord.NewMemStore(pool)
	srv := coord.NewServer(store)

	httpServer := &http.Server{
		Addr:         *listen,
		Handler:      srv,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 35 * time.Second,
		IdleTimeout:  90 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	go func() {
		<-ctx.Done()
		slog.Info("shutting down")
		shutdownCtx, sc := context.WithTimeout(context.Background(), 5*time.Second)
		defer sc()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	slog.Info("gretun-coord listening", "addr", *listen, "pool", pool.String())

	var runErr error
	if *certFile != "" && *keyFile != "" {
		runErr = httpServer.ListenAndServeTLS(*certFile, *keyFile)
	} else {
		runErr = httpServer.ListenAndServe()
	}
	if runErr != nil && !errors.Is(runErr, http.ErrServerClosed) {
		fatal("serve: %v", runErr)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gretun-coord: "+format+"\n", args...)
	os.Exit(1)
}

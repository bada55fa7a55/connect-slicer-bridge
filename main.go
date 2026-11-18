package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "connect-slicer-bridge:", err)
		os.Exit(1)
	}
}

func run() error {
	listen := flag.String("listen", env("LISTEN_ADDR", ":17481"), "address to listen on")
	connectURL := flag.String("connect", env("CONNECT_URL", ""), "base URL of the Prusa Connect server")
	storage := flag.String("storage", env("CONNECT_STORAGE", "local"), "Prusa Connect storage that receives uploads")
	printerStorage := flag.String("printer-storage", env("PRINTER_STORAGE", "usb"), "storage on the printer that files are sent to")
	folder := flag.String("folder", env("UPLOAD_FOLDER", ""), "folder of the Connect storage that keeps uploads")
	temporaryDefault, err := strconv.ParseBool(env("TEMPORARY_UPLOADS", "false"))
	if err != nil {
		return fmt.Errorf("TEMPORARY_UPLOADS must be true or false, not %q", os.Getenv("TEMPORARY_UPLOADS"))
	}
	temporary := flag.Bool("temporary", temporaryDefault, "upload temporary copies that Connect deletes once they reach the printer")
	flag.Parse()

	if *connectURL == "" {
		return errors.New("-connect or CONNECT_URL is required")
	}
	if *temporary && *folder != "" {
		return errors.New("-folder has no effect with -temporary: Connect picks where temporary copies go")
	}
	base, err := url.Parse(*connectURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return fmt.Errorf("invalid Prusa Connect URL %q", *connectURL)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	b := &bridge{
		connect:        &connectClient{base: base, http: &http.Client{}},
		storage:        *storage,
		printerStorage: *printerStorage,
		folder:         strings.Trim(*folder, "/"),
		temporary:      *temporary,
		log:            log,
	}

	srv := &http.Server{
		Addr:              *listen,
		Handler:           b.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	log.Info("listening", "addr", *listen, "connect", base.String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

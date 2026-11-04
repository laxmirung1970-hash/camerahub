package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	mode := env("APP_MODE", "demo")
	if mode != "demo" && mode != "live" {
		log.Fatal("APP_MODE must be demo or live")
	}
	data := env("APP_DATA", "/data")
	store, err := openStore(data, mode)
	if err != nil {
		log.Fatal("cannot open app data: ", err)
	}
	app, err := newApp(store, data, env("GO2RTC_URL", "http://127.0.0.1:1984"))
	if err != nil {
		store.DB.Close()
		log.Fatal(err)
	}
	ln, err := net.Listen("tcp", env("APP_LISTEN", ":8080"))
	if err != nil {
		store.DB.Close()
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log.Printf("CameraHub %s ready on %s; no live discovery until an owner enables a connection", mode, ln.Addr())
	err = serve(ctx, newServer(app.handler(env("APP_WEB", "/web"))), ln, app)
	// Only after every handler and worker has stopped.
	store.DB.Close()
	if err != nil {
		log.Fatal(err)
	}
}
func newServer(h http.Handler) *http.Server {
	// WriteTimeout covers the 60-second discovery request. WebSocket
	// connections are hijacked by the upgrader and clear these deadlines.
	return &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 * 1024}
}

// serve runs the HTTP server and background workers until ctx is cancelled,
// then drains both before returning so the caller can close the database.
func serve(ctx context.Context, server *http.Server, ln net.Listener, app *App) error {
	bgDone := make(chan struct{})
	go func() {
		defer close(bgDone)
		app.background(ctx)
	}()
	served := make(chan error, 1)
	go func() { served <- server.Serve(ln) }()
	select {
	case err := <-served:
		<-bgDone
		return err
	case <-ctx.Done():
	}
	app.stopAll()
	c, done := context.WithTimeout(context.Background(), 8*time.Second)
	defer done()
	err := server.Shutdown(c)
	<-bgDone
	if serveErr := <-served; !errors.Is(serveErr, http.ErrServerClosed) && err == nil {
		err = serveErr
	}
	return err
}

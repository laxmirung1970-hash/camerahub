package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func env(k, f string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return f
}
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
	defer store.DB.Close()
	app, err := newApp(store, data, env("GO2RTC_URL", "http://127.0.0.1:1984"))
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go app.background(ctx)
	server := &http.Server{Addr: env("APP_LISTEN", ":8080"), Handler: app.handler(env("APP_WEB", "/web")), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 32 * 1024}
	go func() {
		<-ctx.Done()
		app.stopAll()
		c, done := context.WithTimeout(context.Background(), 8*time.Second)
		defer done()
		_ = server.Shutdown(c)
	}()
	log.Printf("CameraHub %s ready on %s; no live discovery until an owner enables a connection", mode, server.Addr)
	if err = server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

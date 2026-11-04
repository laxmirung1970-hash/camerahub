package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeDrainsWorkersOnShutdown(t *testing.T) {
	a, h := testApp(t, "demo")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, newServer(h), ln, a) }()
	res, err := http.Get("http://" + ln.Addr().String() + "/api/health")
	if err != nil || res.StatusCode != 200 {
		t.Fatal("server not reachable: ", err)
	}
	res.Body.Close()
	cancel()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return after cancellation")
	}
	if _, err = http.Get("http://" + ln.Addr().String() + "/api/health"); err == nil {
		t.Fatal("listener still open after shutdown")
	}
}

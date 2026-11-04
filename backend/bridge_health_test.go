package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGatewayHealthRequiresFullStartup(t *testing.T) {
	response := `{"startup":"starting:xiaomi"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(response)) }))
	defer server.Close()
	bridge := &Bridge{Base: server.URL, Client: server.Client()}
	if bridge.healthy(context.Background()) {
		t.Fatal("reported partial startup ready")
	}
	response = `{"version":"fixture"}`
	if bridge.healthy(context.Background()) {
		t.Fatal("API alone is not full startup")
	}
	response = `{"startup":"ready"}`
	if !bridge.healthy(context.Background()) {
		t.Fatal("ready gateway rejected")
	}
}

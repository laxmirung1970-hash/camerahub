package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRelayAllowlistOnlySavedPrivateXiaomi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "relay-hosts.json")
	t.Setenv("CAMERAHUB_RELAY_HOSTS_FILE", path)
	a, _ := testApp(t, "live")
	for _, c := range []Camera{{ID: "xiaomi-one", Vendor: "xiaomi", Host: "172.16.0.202"}, {ID: "same-host", Vendor: "xiaomi", Host: "172.16.0.202"}, {ID: "other-private", Vendor: "xiaomi", Host: "10.1.1.4"}, {ID: "public", Vendor: "xiaomi", Host: "8.8.8.8"}, {ID: "tapo", Vendor: "tapo", Host: "172.16.0.92"}} {
		if a.store.saveCamera(c) != nil {
			t.Fatal("fixture")
		}
	}
	if err := a.writeRelayHosts(); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	var hosts []string
	if json.Unmarshal(raw, &hosts) != nil || !reflect.DeepEqual(hosts, []string{"10.1.1.4", "172.16.0.202"}) {
		t.Fatal("unexpected relay destinations")
	}
	a.store.DB.Exec("DELETE FROM cameras WHERE id IN ('xiaomi-one','same-host')")
	if err := a.writeRelayHosts(); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(path)
	json.Unmarshal(raw, &hosts)
	if !reflect.DeepEqual(hosts, []string{"10.1.1.4"}) {
		t.Fatal("removed cameras retained access")
	}
}

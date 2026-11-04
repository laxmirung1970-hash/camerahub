package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sort"
)

// A Windows-only transport allowlist derived solely from the saved registry.
// Never include account credentials or arbitrary LAN addresses in this file.
func (a *App) writeRelayHosts() error {
	path := a.cfg.relayHostsFile
	if path == "" || a.mode != "live" {
		return nil
	}
	cameras, err := a.store.cameras()
	if err != nil {
		return err
	}
	hosts := []string{}
	seen := map[string]bool{}
	for _, c := range cameras {
		ip := net.ParseIP(c.Host)
		if c.Vendor != "xiaomi" || ip == nil || ip.To4() == nil || !ip.IsPrivate() || seen[c.Host] {
			continue
		}
		hosts = append(hosts, c.Host)
		seen[c.Host] = true
	}
	sort.Strings(hosts)
	raw, _ := json.Marshal(hosts)
	if old, err := os.ReadFile(path); err == nil && string(old) == string(raw) {
		return nil
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".relay-hosts-*")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temp, path)
}

package main

import (
	"fmt"
	"testing"
	"time"
)

func newTestWatch() *pgWatchState {
	return &pgWatchState{
		registeredHosts: map[string]string{
			"db.example.com:5432": "conn-1",
			"[2001:db8::1]:5432":  "conn-6",
		},
		gatewayAddrs: map[string]bool{"127.0.0.1:6432": true},
		lastReport:   map[string]time.Time{},
	}
}

func TestClassifyObserved(t *testing.T) {
	now := time.Now()

	t.Run("registered peer is a direct connection", func(t *testing.T) {
		w := newTestWatch()
		kind, id, report := w.classifyObserved(pgObservedConn{PeerHost: "db.example.com", PeerPort: 5432}, now)
		if !report || kind != "pg_direct_connection" || id != "conn-1" {
			t.Errorf("got (%q,%q,%v)", kind, id, report)
		}
	})

	t.Run("unknown peer on a pg port is unregistered", func(t *testing.T) {
		w := newTestWatch()
		kind, id, report := w.classifyObserved(pgObservedConn{PeerHost: "other.example.com", PeerPort: 6543}, now)
		if !report || kind != "pg_unregistered_connection" || id != "" {
			t.Errorf("got (%q,%q,%v)", kind, id, report)
		}
	})

	t.Run("gateway listener is never reported", func(t *testing.T) {
		w := newTestWatch()
		if _, _, report := w.classifyObserved(pgObservedConn{PeerHost: "127.0.0.1", PeerPort: 6432}, now); report {
			t.Error("the proxy itself must not be reported")
		}
	})

	t.Run("non-pg port ignored", func(t *testing.T) {
		w := newTestWatch()
		if _, _, report := w.classifyObserved(pgObservedConn{PeerHost: "db.example.com", PeerPort: 443}, now); report {
			t.Error("port 443 is not a pg port")
		}
	})

	t.Run("debounce suppresses repeats inside the window", func(t *testing.T) {
		w := newTestWatch()
		c := pgObservedConn{PeerHost: "db.example.com", PeerPort: 5432}
		if _, _, r := w.classifyObserved(c, now); !r {
			t.Fatal("first must report")
		}
		if _, _, r := w.classifyObserved(c, now.Add(time.Minute)); r {
			t.Error("repeat inside the window must be suppressed")
		}
		if _, _, r := w.classifyObserved(c, now.Add(pgWatchDebounce+time.Second)); !r {
			t.Error("past the window it must report again")
		}
	})
}

func TestParseLsofConns(t *testing.T) {
	out := "p123\nf12\nn127.0.0.1:54321->10.0.0.5:5432\nf13\nn[::1]:9999->[2001:db8::1]:6543\nnno-arrow-line\nn127.0.0.1:1->10.0.0.5:5432\n"
	conns := parseLsofConns(out)
	if len(conns) != 2 {
		t.Fatalf("conns = %+v, want 2 (deduped)", conns)
	}
	if conns[0].PeerHost != "10.0.0.5" || conns[0].PeerPort != 5432 {
		t.Errorf("first = %+v", conns[0])
	}
	if conns[1].PeerHost != "2001:db8::1" || conns[1].PeerPort != 6543 {
		t.Errorf("second = %+v", conns[1])
	}
}

func TestPgWatchHostsRoundTrip(t *testing.T) {
	raw := "db.example.com:5432=conn-1,warehouse.example.com:6543=conn-2"
	m := parsePgWatchHosts(raw)
	if len(m) != 2 || m["db.example.com:5432"] != "conn-1" || m["warehouse.example.com:6543"] != "conn-2" {
		t.Errorf("parsed = %v", m)
	}
	if parsePgWatchHosts("")["x"] != "" || len(parsePgWatchHosts("")) != 0 {
		t.Error("empty input must parse to empty map")
	}
	if len(parsePgWatchHosts("garbage,also=,=x")) != 0 {
		t.Errorf("malformed entries must be dropped: %v", parsePgWatchHosts("garbage,also=,=x"))
	}
}

func TestExpandWatchHostsDNS(t *testing.T) {
	hosts := map[string]string{
		"db.example.com:5432": "conn-1",
		"10.0.0.9:5432":       "conn-2", // IP literal: no lookup
	}
	var lookedUp []string
	expanded := expandWatchHostsDNS(hosts, func(host string) ([]string, error) {
		lookedUp = append(lookedUp, host)
		return []string{"10.0.0.5", "2001:db8::1"}, nil
	})
	if len(lookedUp) != 1 || lookedUp[0] != "db.example.com" {
		t.Errorf("lookups = %v, want only the hostname entry", lookedUp)
	}
	for _, key := range []string{"db.example.com:5432", "10.0.0.5:5432", "[2001:db8::1]:5432"} {
		if expanded[key] != "conn-1" {
			t.Errorf("missing expanded key %q: %v", key, expanded)
		}
	}
	if expanded["10.0.0.9:5432"] != "conn-2" {
		t.Error("IP-literal entry must survive unchanged")
	}
	// Lookup failure keeps the original entry.
	failed := expandWatchHostsDNS(map[string]string{"gone.example.com:5432": "c"}, func(string) ([]string, error) {
		return nil, fmt.Errorf("nxdomain")
	})
	if failed["gone.example.com:5432"] != "c" || len(failed) != 1 {
		t.Errorf("failure case = %v", failed)
	}
}

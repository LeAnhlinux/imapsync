package services

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/imapsync/web-backend/internal/store"
	"net"
	"strings"
	"testing"
)

func TestPrivateAddressesAreNotPublic(t *testing.T) {
	// A webhook URL is user-supplied and this process runs beside the database
	// and the key that decrypts every stored mailbox password. These are the
	// addresses a request must never be able to reach from here.
	blocked := []string{
		"127.0.0.1", "::1", // loopback
		"10.0.0.5", "192.168.1.1", "172.16.0.1", // RFC1918
		"169.254.169.254", // cloud metadata
		"fe80::1",         // link-local v6
		"0.0.0.0", "::",   // unspecified
		"100.64.0.1", "100.127.255.1", // carrier-grade NAT
		"224.0.0.1", // multicast
		"fd00::1",   // unique local
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("bad test address %q", s)
		}
		if isPublicIP(ip) {
			t.Errorf("%s was treated as a public address", s)
		}
	}

	for _, s := range []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:2800:220:1:248:1893:25c8:1946"} {
		if !isPublicIP(net.ParseIP(s)) {
			t.Errorf("%s was refused, but it is a normal public address", s)
		}
	}
	// 100.63 and 100.128 are outside the CGNAT block and must still be allowed.
	for _, s := range []string{"100.63.255.255", "100.128.0.1"} {
		if !isPublicIP(net.ParseIP(s)) {
			t.Errorf("%s is outside 100.64.0.0/10 but was refused", s)
		}
	}
}

func TestDialRefusesLoopback(t *testing.T) {
	// The check has to happen at dial time, not on the URL string: this is what
	// also stops a hostname that resolves inward and a redirect chain that ends
	// somewhere private.
	_, err := dialPublicOnly(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil {
		t.Fatal("dialled loopback")
	}
	if !strings.Contains(err.Error(), "not a public address") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	// localhost resolves to loopback, so the name must be refused too.
	if _, err := dialPublicOnly(context.Background(), "tcp", "localhost:80"); err == nil {
		t.Error("dialled localhost")
	}
}

func TestSchedulesFireInTheZoneTimestampsUse(t *testing.T) {
	// cron.New() defaults to time.Local. The server runs UTC and every
	// timestamp in this application is UTC+7, so a user's "0 2 * * *" fired at
	// 09:00 Vietnam time — and History then showed it as having run at 09:00,
	// which looks like the scheduler is broken rather than mis-zoned.
	sched, err := cron.ParseStandard("0 2 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// From 00:00 UTC+7 the next 2am must be two hours later, not nine.
	from := time.Date(2026, 9, 4, 0, 0, 0, 0, store.VN)
	next := sched.Next(from)
	if got := next.In(store.VN).Hour(); got != 2 {
		t.Errorf("the next run is at %02d:00 Vietnam time, want 02:00", got)
	}
	if d := next.Sub(from); d != 2*time.Hour {
		t.Errorf("that is %v away, want 2h", d)
	}
}

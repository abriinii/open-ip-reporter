package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"betteripreporter/internal/capture"
	"betteripreporter/internal/export"
	"betteripreporter/internal/parse"
)

// TestRealSocketToCSV drives the whole chain the way a walk does: a real UDP
// datagram on a real socket, through the parser, into a session, and out to a
// file on disk.
//
// Every other test in this package calls onPacket directly, which skips the
// part most likely to be wrong in practice — binding, receiving, and the
// double-fire arriving as two separate datagrams rather than two function
// calls.
func TestRealSocketToCSV(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B1", 3)

	stop := make(chan struct{})
	defer close(stop)

	received := make(chan struct{}, 16)
	bound, err := capture.Listen([]int{parse.AntminerPort}, func(p capture.Packet) {
		a.onPacket(p)
		received <- struct{}{}
	}, stop)
	if err != nil {
		t.Fatalf("could not bind udp/%d: %v", parse.AntminerPort, err)
	}
	if bound != 1 {
		t.Fatalf("bound %d sockets, want 1", bound)
	}

	send := func(payload string) {
		t.Helper()
		conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", parse.AntminerPort))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		if _, err := conn.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	await := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			select {
			case <-received:
			case <-time.After(3 * time.Second):
				t.Fatalf("packet %d never arrived", i+1)
			}
		}
	}

	// Two machines, each firing twice a second apart exactly as they do on site.
	send("21.1.1.43,02:81:f5:ea:e1:db")
	send("21.1.1.43,02:81:f5:ea:e1:db")
	await(2)

	send("21.1.11.232,02:ad:af:02:ff:45")
	send("21.1.11.232,02:ad:af:02:ff:45")
	await(2)

	st := a.State()
	if len(st.Entries) != 2 {
		t.Fatalf("four datagrams produced %d rows, want 2 — the double-fire was not collapsed", len(st.Entries))
	}
	if st.Entries[0].MAC != "02:81:f5:ea:e1:db" || st.Entries[0].Label != "C1/1" {
		t.Errorf("row 1 = %s at %s", st.Entries[0].MAC, st.Entries[0].Label)
	}
	if st.Entries[1].MAC != "02:ad:af:02:ff:45" || st.Entries[1].Label != "C1/2" {
		t.Errorf("row 2 = %s at %s", st.Entries[1].MAC, st.Entries[1].Label)
	}
	if st.NextLabel != "C1/3" {
		t.Errorf("next position %s, want C1/3", st.NextLabel)
	}

	// A skip with a note, then a third machine, so the export exercises the
	// blank row and the notes column together.
	a.Skip()
	a.SetNote(2, "wont ip report")
	send("21.1.1.55,02:44:1c:99:0b:7a")
	await(1)

	// The session must already be on disk without anyone asking: a closed lid
	// mid-rack is the case persistence exists for.
	saved := filepath.Join(a.sessionDir, "B1-rack3.json")
	if _, err := os.Stat(saved); err != nil {
		t.Fatalf("session was not saved as it went: %v", err)
	}

	out := filepath.Join(t.TempDir(), "B1-rack3.csv")
	if err := export.WriteFile(out, a.session, export.Positional); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}

	want := "21.1.1.43,02:81:f5:ea:e1:db,\n" +
		"21.1.11.232,02:ad:af:02:ff:45,\n" +
		",,wont ip report\n" +
		"21.1.1.55,02:44:1c:99:0b:7a,\n"
	if string(data) != want {
		t.Errorf("exported file:\n%s\nwant:\n%s", data, want)
	}
	if lines := strings.Count(string(data), "\n"); lines != 4 {
		t.Errorf("file has %d rows, want 4 — one per position walked", lines)
	}
}

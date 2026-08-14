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

// Both vendors through one listener, on their own ports and formats. This is
// the thing that justifies replacing two separate vendor tools with one.
func TestBothVendorsLandInOneRack(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B2", 1)

	stop := make(chan struct{})
	defer close(stop)

	got := make(chan struct{}, 8)
	if _, err := capture.Listen(
		[]int{parse.AntminerPort, parse.WhatsminerPort},
		func(p capture.Packet) { a.onPacket(p); got <- struct{}{} },
		stop,
	); err != nil {
		t.Fatalf("bind: %v", err)
	}

	send := func(port int, payload string) {
		t.Helper()
		c, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.Write([]byte(payload))
	}
	await := func(n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			select {
			case <-got:
			case <-time.After(3 * time.Second):
				t.Fatalf("packet %d never arrived", i+1)
			}
		}
	}

	send(parse.AntminerPort, "22.1.1.10,02:81:f5:ea:e1:db")
	await(1)
	send(parse.WhatsminerPort, "IP:22.1.1.11MAC:00:0a:35:1b:2c:3d")
	await(1)
	send(parse.AntminerPort, "22.1.1.12,02:ad:af:02:ff:45")
	await(1)

	st := a.State()
	if len(st.Entries) != 3 {
		t.Fatalf("got %d rows, want 3 (two vendors interleaved)", len(st.Entries))
	}
	want := []string{"02:81:f5:ea:e1:db", "00:0a:35:1b:2c:3d", "02:ad:af:02:ff:45"}
	for i, mac := range want {
		if st.Entries[i].MAC != mac {
			t.Errorf("row %d = %s, want %s", i+1, st.Entries[i].MAC, mac)
		}
	}
	if st.Entries[1].Label != "C1/2" {
		t.Errorf("the Whatsminer landed at %s, want C1/2", st.Entries[1].Label)
	}
}

// Stopping must keep the rack so it can be exported, and must stop recording.
func TestStopKeepsTheRackButStopsRecording(t *testing.T) {
	a := newTestApp(t)
	a.StartSession("B2", 1)
	a.onPacket(antminerPacket("22.1.1.10", "02:81:f5:ea:e1:db", time.Now()))

	st := a.StopSession()
	if st.Active {
		t.Error("still walking after Stop")
	}
	if !st.HasSession || len(st.Entries) != 1 {
		t.Fatalf("Stop discarded the rack: hasSession=%v rows=%d", st.HasSession, len(st.Entries))
	}

	// A press after Stop must not be recorded.
	a.onPacket(antminerPacket("22.1.1.11", "02:ad:af:02:ff:45", time.Now()))
	if n := len(a.State().Entries); n != 1 {
		t.Errorf("recorded %d rows after Stop, want 1", n)
	}
}

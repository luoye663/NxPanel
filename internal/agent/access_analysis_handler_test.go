package agent

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/luoye663/nxpanel/internal/accessanalysis"
	"github.com/luoye663/nxpanel/internal/app"
)

func TestReadBoundedLineDoesNotRetainOversizedInput(t *testing.T) {
	reader := bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", 1024*1024)+"\nnext\n"), 1024)
	line, consumed, oversized, err := readBoundedLine(reader, 128)
	if err != nil || !oversized || len(line) != 128 || consumed != 1024*1024+1 {
		t.Fatalf("oversized line result len=%d consumed=%d oversized=%v err=%v", len(line), consumed, oversized, err)
	}
	next, _, oversized, err := readBoundedLine(reader, 128)
	if err != nil || oversized || string(next) != "next\n" {
		t.Fatalf("reader did not recover after oversized line: %q oversized=%v err=%v", next, oversized, err)
	}
}

func TestAccessScanSharesLineBudgetAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	line := `127.0.0.1 - - [26/Jul/2026:03:00:00 +0000] "GET / HTTP/1.1" 200 12 "-" "test"` + "\n"
	paths := []string{filepath.Join(dir, "one.log"), filepath.Join(dir, "two.log")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte(line+line), 0644); err != nil {
			t.Fatal(err)
		}
	}
	parser, err := accessanalysis.NewParser("combined", "", false)
	if err != nil {
		t.Fatal(err)
	}
	agg := accessanalysis.NewAggregator(time.Unix(0, 0), time.Now().Add(24*time.Hour))
	budget := &accessScanBudget{bytesRemaining: 1 << 20, linesRemaining: 3}
	var total int64
	for _, path := range paths {
		_, scanned, skipped, _, parseErrors := scanAccessLogFile(context.Background(), path, parser, agg, accessanalysis.Cursor{}, budget, 32*1024)
		if skipped != 0 || len(parseErrors) != 0 {
			t.Fatalf("unexpected skipped/errors: %d %v", skipped, parseErrors)
		}
		total += scanned
	}
	if total != 3 || budget.linesRemaining != 0 {
		t.Fatalf("shared line budget not enforced: scanned=%d remaining=%d", total, budget.linesRemaining)
	}
}

func TestAccessScanCancellationStopsBeforeReading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	if err := os.WriteFile(path, []byte(strings.Repeat("line\n", 100)), 0644); err != nil {
		t.Fatal(err)
	}
	parser, _ := accessanalysis.NewParser("combined", "", false)
	agg := accessanalysis.NewAggregator(time.Unix(0, 0), time.Now().Add(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := &accessScanBudget{bytesRemaining: 1024, linesRemaining: 100}
	_, scanned, _, truncated, _ := scanAccessLogFile(ctx, path, parser, agg, accessanalysis.Cursor{}, budget, 128)
	if scanned != 0 || !truncated || budget.bytesRemaining != 1024 {
		t.Fatalf("cancelled scan consumed data: scanned=%d truncated=%v remaining=%d", scanned, truncated, budget.bytesRemaining)
	}
}

func TestAccessScanBudgetCutReprocessesCompleteLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "access.log")
	line1 := `127.0.0.1 - - [26/Jul/2026:03:00:00 +0000] "GET /one HTTP/1.1" 200 12 "-" "test"` + "\n"
	line2 := `127.0.0.1 - - [26/Jul/2026:03:00:01 +0000] "GET /two HTTP/1.1" 200 12 "-" "test"` + "\n"
	line3 := `127.0.0.1 - - [26/Jul/2026:03:00:02 +0000] "GET /three HTTP/1.1" 200 12 "-" "test"`
	if err := os.WriteFile(path, []byte(line1+line2+line3), 0644); err != nil {
		t.Fatal(err)
	}
	parser, err := accessanalysis.NewParser("combined", "", false)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 7, 26, 2, 0, 0, 0, time.UTC)
	to := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	agg := accessanalysis.NewAggregator(from, to)
	firstBudget := &accessScanBudget{bytesRemaining: int64(len(line1) + len(line2)/2), linesRemaining: 10}
	cursor, scanned, skipped, truncated, parseErrors := scanAccessLogFile(context.Background(), path, parser, agg, accessanalysis.Cursor{}, firstBudget, 32*1024)
	if scanned != 1 || skipped != 0 || !truncated || len(parseErrors) != 0 {
		t.Fatalf("first scan scanned=%d skipped=%d truncated=%v errors=%v", scanned, skipped, truncated, parseErrors)
	}
	if cursor.Offset != int64(len(line1)) {
		t.Fatalf("cursor advanced past partial line: got %d want %d", cursor.Offset, len(line1))
	}

	secondBudget := &accessScanBudget{bytesRemaining: 1 << 20, linesRemaining: 10}
	_, scanned, skipped, truncated, parseErrors = scanAccessLogFile(context.Background(), path, parser, agg, cursor, secondBudget, 32*1024)
	if scanned != 2 || skipped != 0 || truncated || len(parseErrors) != 0 {
		t.Fatalf("second scan scanned=%d skipped=%d truncated=%v errors=%v", scanned, skipped, truncated, parseErrors)
	}
	result := agg.Result()
	if len(result.Paths) != 3 {
		t.Fatalf("expected each path exactly once, got %+v", result.Paths)
	}
}

func TestAccessScanAggregationLimitsAreHardClamped(t *testing.T) {
	s := &Server{cfg: &app.Config{Agent: app.AgentConfig{Resources: app.AgentResourceConfig{
		AccessScanMaxPaths: -1, AccessScanMaxIPs: 999999, AccessScanMaxHourly: 1,
		AccessScanMaxAnomalies: 999999, AccessScanMaxEntries: 999999,
		AccessScanMaxDistinct: 999999,
	}}}}
	limits := s.accessScanLimits().aggregation
	if limits.Paths != 10000 || limits.IPs != 50000 || limits.Hourly != 24 || limits.Anomalies != 10000 || limits.Entries != 100000 || limits.TotalDistinct != 200000 {
		t.Fatalf("unexpected hard clamps: %+v", limits)
	}
}

func TestRequestedAggregationLimitsControlEntryCollection(t *testing.T) {
	configured := accessanalysis.DefaultAggregationLimits()
	disabledReq := &accessanalysis.AgentScanRequest{CollectEntries: false, MaxEntries: 50000}
	disabled := requestedAggregationLimits(disabledReq, configured)
	if disabled.Entries != 0 || disabledReq.MaxEntries != 0 {
		t.Fatalf("disabled limits=%+v request=%+v", disabled, disabledReq)
	}
	requestedReq := &accessanalysis.AgentScanRequest{CollectEntries: true, MaxEntries: 1234}
	requested := requestedAggregationLimits(requestedReq, configured)
	if requested.Entries != 1234 || requestedReq.MaxEntries != 1234 {
		t.Fatalf("requested limits=%+v request=%+v", requested, requestedReq)
	}
	oversizedReq := &accessanalysis.AgentScanRequest{CollectEntries: true, MaxEntries: configured.Entries + 1}
	bounded := requestedAggregationLimits(oversizedReq, configured)
	if bounded.Entries != configured.Entries || oversizedReq.MaxEntries != configured.Entries {
		t.Fatalf("bounded limits=%+v request=%+v", bounded, oversizedReq)
	}
}

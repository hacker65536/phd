package query

import (
	"testing"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

func TestApply_FiltersByServiceAndStatus(t *testing.T) {
	events := []model.Event{
		{EventTypeCode: "A", Service: "EC2", StatusCode: "open", Region: "us-east-1"},
		{EventTypeCode: "B", Service: "RDS", StatusCode: "open", Region: "us-east-1"},
		{EventTypeCode: "C", Service: "EC2", StatusCode: "closed", Region: "ap-northeast-1"},
	}
	re, _ := CompileEventType("")
	f := Filter{Service: "ec2", Status: "open", EventType: re} // 大文字小文字無視
	got := f.Apply(events)
	if len(got) != 1 || got[0].EventTypeCode != "A" {
		t.Fatalf("got %+v, want only event A", got)
	}
}

func TestApply_CategoryCSVMultiple(t *testing.T) {
	events := []model.Event{
		{EventTypeCode: "A", Category: "scheduledChange"},
		{EventTypeCode: "B", Category: "accountNotification"},
		{EventTypeCode: "C", Category: "issue"},
	}
	// 単一
	if got := (Filter{Category: "scheduledChange"}).Apply(events); len(got) != 1 || got[0].EventTypeCode != "A" {
		t.Fatalf("single category: got %+v", got)
	}
	// カンマ区切り複数（大文字小文字・空白無視）
	got := (Filter{Category: "scheduledChange, accountNotification"}).Apply(events)
	if len(got) != 2 {
		t.Fatalf("multi category: got %d, want 2", len(got))
	}
}

func TestApply_EventTypeRegex(t *testing.T) {
	events := []model.Event{
		{EventTypeCode: "AWS_EC2_INSTANCE_RETIREMENT_SCHEDULED"},
		{EventTypeCode: "AWS_RDS_MAINTENANCE_SCHEDULED"},
	}
	re, err := CompileEventType("RETIREMENT")
	if err != nil {
		t.Fatal(err)
	}
	got := Filter{EventType: re}.Apply(events)
	if len(got) != 1 || got[0].EventTypeCode != "AWS_EC2_INSTANCE_RETIREMENT_SCHEDULED" {
		t.Fatalf("got %+v, want retirement event", got)
	}
}

func TestPruneStaleOpen(t *testing.T) {
	now := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	fresh := now.AddDate(0, 0, -10)
	stale := now.AddDate(0, 0, -200)
	events := []model.Event{
		{EventTypeCode: "open-fresh", StatusCode: "open", LastUpdated: fresh},
		{EventTypeCode: "open-stale", StatusCode: "open", LastUpdated: stale},
		{EventTypeCode: "upcoming-stale", StatusCode: "upcoming", LastUpdated: stale}, // 対象外で残る
		{EventTypeCode: "open-unknown", StatusCode: "open"},                           // lastUpdated 不明は残す
	}
	got := PruneStaleOpen(events, now, 90*24*time.Hour)
	keep := map[string]bool{}
	for _, e := range got {
		keep[e.EventTypeCode] = true
	}
	if keep["open-stale"] {
		t.Error("stale open should be pruned")
	}
	for _, code := range []string{"open-fresh", "upcoming-stale", "open-unknown"} {
		if !keep[code] {
			t.Errorf("%s should be kept", code)
		}
	}
}

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 6, 9, 12, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"-30d":       now.AddDate(0, 0, -30),
		"7d":         now.AddDate(0, 0, -7),
		"2w":         now.Add(-14 * 24 * time.Hour),
		"12h":        now.Add(-12 * time.Hour),
		"2026-01-01": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := ParseSince(in, now)
		if err != nil {
			t.Errorf("ParseSince(%q) error: %v", in, err)
			continue
		}
		if !got.Equal(want) {
			t.Errorf("ParseSince(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseSince("garbage", now); err == nil {
		t.Error("ParseSince(garbage) should error")
	}
}

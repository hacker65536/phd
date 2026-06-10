package merge

import (
	"testing"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

func ev(code, region, status string, start time.Time) model.Event {
	return model.Event{
		EventTypeCode: code,
		Service:       "EC2",
		Category:      "scheduledChange",
		Region:        region,
		StatusCode:    status,
		StartTime:     start,
	}
}

func TestByEventType_MergesSameScheduleAcrossRegions(t *testing.T) {
	// 同一 startTime の region コピーは束ね、別 startTime の occurrence は分離する。
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	events := []model.Event{
		ev("AWS_EC2_RETIREMENT", "us-east-1", "upcoming", t0),
		ev("AWS_EC2_RETIREMENT", "ap-northeast-1", "open", t0),     // 同日程・別region → 束ねる
		ev("AWS_EC2_RETIREMENT", "ap-northeast-1", "upcoming", t1), // 別日程 → 分離
	}

	got := ByEventType(events)

	if len(got) != 2 {
		t.Fatalf("logical events = %d, want 2 (same-schedule merged, different schedule separate)", len(got))
	}

	merged := got[0] // t0 の occurrence（最初の出現順）
	if len(merged.Regions) != 2 || merged.Regions[0] != "ap-northeast-1" || merged.Regions[1] != "us-east-1" {
		t.Errorf("regions = %v, want [ap-northeast-1 us-east-1] (sorted union)", merged.Regions)
	}
	if merged.StatusCode != "open" {
		t.Errorf("status = %q, want open (most severe wins)", merged.StatusCode)
	}
	if !merged.StartTime.Equal(t0) {
		t.Errorf("startTime = %v, want %v", merged.StartTime, t0)
	}
	if len(merged.RawEvents) != 2 {
		t.Errorf("rawEvents = %d, want 2", len(merged.RawEvents))
	}
	if !got[1].StartTime.Equal(t1) {
		t.Errorf("second occurrence startTime = %v, want %v", got[1].StartTime, t1)
	}
}

func TestNoMerge_KeepsEachEvent(t *testing.T) {
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	events := []model.Event{
		ev("AWS_EC2_RETIREMENT", "us-east-1", "upcoming", t0),
		ev("AWS_EC2_RETIREMENT", "ap-northeast-1", "open", t0),
	}
	got := NoMerge(events)
	if len(got) != 2 {
		t.Fatalf("no-merge events = %d, want 2", len(got))
	}
	if len(got[0].Regions) != 1 || got[0].Regions[0] != "us-east-1" {
		t.Errorf("regions[0] = %v, want [us-east-1]", got[0].Regions)
	}
}

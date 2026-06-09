package group

import (
	"testing"
	"time"

	"phd/internal/model"
)

func TestByEventType_RollsUpOccurrences(t *testing.T) {
	t1 := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	events := []model.LogicalEvent{
		{EventTypeCode: "AWS_ECS_PATCH", Service: "ECS", Category: "scheduledChange", StatusCode: "upcoming", Regions: []string{"us-east-1"}, StartTime: t2},
		{EventTypeCode: "AWS_ECS_PATCH", Service: "ECS", Category: "scheduledChange", StatusCode: "upcoming", Regions: []string{"ap-northeast-1"}, StartTime: t1},
		{EventTypeCode: "AWS_ECS_PATCH", Service: "ECS", Category: "scheduledChange", StatusCode: "open", Regions: []string{"us-west-2"}, StartTime: t1},
		{EventTypeCode: "AWS_RDS_LIFECYCLE", Service: "RDS", Category: "scheduledChange", StatusCode: "upcoming", Regions: []string{"ap-northeast-1"}, StartTime: t2},
	}

	got := ByEventType(events)
	if len(got) != 2 {
		t.Fatalf("groups = %d, want 2", len(got))
	}

	ecs := got[0] // open を含むので最優先
	if ecs.EventTypeCode != "AWS_ECS_PATCH" {
		t.Fatalf("first group = %q, want AWS_ECS_PATCH (has open)", ecs.EventTypeCode)
	}
	if ecs.OccurrenceCount != 3 {
		t.Errorf("occurrences = %d, want 3", ecs.OccurrenceCount)
	}
	if len(ecs.Regions) != 3 {
		t.Errorf("regions = %v, want 3 (union)", ecs.Regions)
	}
	if ecs.StatusCounts["upcoming"] != 2 || ecs.StatusCounts["open"] != 1 {
		t.Errorf("statusCounts = %v, want upcoming:2 open:1", ecs.StatusCounts)
	}
	if !ecs.NextStart.Equal(t1) {
		t.Errorf("nextStart = %v, want %v (soonest upcoming)", ecs.NextStart, t1)
	}
}

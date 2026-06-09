// Package merge は region をまたぐ同一 eventTypeCode のイベントを論理イベントへ束ねる。
package merge

import (
	"sort"
	"time"

	"phd/internal/model"
)

// statusSeverity は status の深刻度。混在時の代表値選択に使う。
var statusSeverity = map[string]int{"open": 3, "upcoming": 2, "closed": 1}

// mergeKey は (eventTypeCode, startTime) を1つの論理イベントの識別子にする。
// 同一スケジュールの region コピーだけを束ね、別日程の occurrence は分離する。
func mergeKey(e model.Event) string {
	return e.EventTypeCode + "\x00" + e.StartTime.UTC().Format(time.RFC3339)
}

// ByEventType は (eventTypeCode, startTime) 単位でイベントをマージする。
// region は集合和、status は最も深刻なものを代表、終了時刻は最大とする。
// 入力の最初の出現順を保持する。
func ByEventType(events []model.Event) []model.LogicalEvent {
	order := make([]string, 0)
	byKey := make(map[string]*model.LogicalEvent)
	regionSet := make(map[string]map[string]bool)

	for _, e := range events {
		key := mergeKey(e)
		le, ok := byKey[key]
		if !ok {
			le = &model.LogicalEvent{
				EventTypeCode: e.EventTypeCode,
				Service:       e.Service,
				Category:      e.Category,
				StatusCode:    e.StatusCode,
				StartTime:     e.StartTime,
				EndTime:       e.EndTime,
			}
			byKey[key] = le
			regionSet[key] = make(map[string]bool)
			order = append(order, key)
		}
		if e.Region != "" {
			regionSet[key][e.Region] = true
		}
		if !e.StartTime.IsZero() && (le.StartTime.IsZero() || e.StartTime.Before(le.StartTime)) {
			le.StartTime = e.StartTime
		}
		if e.EndTime.After(le.EndTime) {
			le.EndTime = e.EndTime
		}
		if statusSeverity[e.StatusCode] > statusSeverity[le.StatusCode] {
			le.StatusCode = e.StatusCode
		}
		le.RawEvents = append(le.RawEvents, e)
	}

	out := make([]model.LogicalEvent, 0, len(order))
	for _, key := range order {
		le := byKey[key]
		regions := make([]string, 0, len(regionSet[key]))
		for r := range regionSet[key] {
			regions = append(regions, r)
		}
		sort.Strings(regions)
		le.Regions = regions
		out = append(out, *le)
	}
	return out
}

// NoMerge は各イベントをマージせず 1 論理イベントとして包む（--no-merge 用）。
func NoMerge(events []model.Event) []model.LogicalEvent {
	out := make([]model.LogicalEvent, 0, len(events))
	for _, e := range events {
		le := model.LogicalEvent{
			EventTypeCode: e.EventTypeCode,
			Service:       e.Service,
			Category:      e.Category,
			StatusCode:    e.StatusCode,
			StartTime:     e.StartTime,
			EndTime:       e.EndTime,
			RawEvents:     []model.Event{e},
		}
		if e.Region != "" {
			le.Regions = []string{e.Region}
		}
		out = append(out, le)
	}
	return out
}

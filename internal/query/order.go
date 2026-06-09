package query

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"

	"phd/internal/model"
)

// statusRank はアクション優先度。進行中(open) を最優先、次に予定(upcoming)、済み(closed)。
func statusRank(s string) int {
	switch s {
	case "open":
		return 0
	case "upcoming":
		return 1
	case "closed":
		return 2
	default:
		return 3
	}
}

// SortLogical はアクション優先度（status）→ 開始時刻昇順 で並べ替える。
// 「今動いているもの → 開始が近い予定 → 先の予定」の順になる。
func SortLogical(events []model.LogicalEvent) {
	sort.SliceStable(events, func(i, j int) bool {
		ri, rj := statusRank(events[i].StatusCode), statusRank(events[j].StatusCode)
		if ri != rj {
			return ri < rj
		}
		return events[i].StartTime.Before(events[j].StartTime)
	})
}

// ApplyHorizon は前方ホライズン（開始が now+within 以内）でイベントを絞る。
// 進行中(open) と開始時刻不明のイベントは常に残す（未来の上限のみを課す）。
func ApplyHorizon(events []model.Event, now time.Time, within time.Duration) []model.Event {
	limit := now.Add(within)
	out := make([]model.Event, 0, len(events))
	for _, e := range events {
		if e.StatusCode == "open" || e.StartTime.IsZero() || !e.StartTime.After(limit) {
			out = append(out, e)
		}
	}
	return out
}

// PruneStaleOpen は open イベントのうち lastUpdated が古いものを落とす。
// upcoming/closed は対象外。lastUpdated 不明の open は安全側で残す。
// 長期間居座る planned-lifecycle 通知のノイズを既定ビューから除くために使う。
func PruneStaleOpen(events []model.Event, now time.Time, openSince time.Duration) []model.Event {
	cutoff := now.Add(-openSince)
	out := make([]model.Event, 0, len(events))
	for _, e := range events {
		if e.StatusCode == "open" && !e.LastUpdated.IsZero() && e.LastUpdated.Before(cutoff) {
			continue
		}
		out = append(out, e)
	}
	return out
}

var durPattern = regexp.MustCompile(`^(\d+)([mhdw])$`)

// ParseDuration は 30m / 12h / 90d / 2w を time.Duration に変換する。
func ParseDuration(s string) (time.Duration, error) {
	m := durPattern.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (use 30m / 12h / 90d / 2w)", s)
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "m":
		return time.Duration(n) * time.Minute, nil
	case "h":
		return time.Duration(n) * time.Hour, nil
	case "d":
		return time.Duration(n) * 24 * time.Hour, nil
	case "w":
		return time.Duration(n) * 7 * 24 * time.Hour, nil
	}
	return 0, fmt.Errorf("invalid duration %q", s)
}

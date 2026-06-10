// Package query はイベントの期間パースと絞り込みを担う（マージ前の生イベントに適用）。
package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

// Filter は生イベントへの絞り込み条件。空フィールドは無視される。
// Service / Category / Region はカンマ区切りで複数指定できる（いずれかに一致でヒット）。
type Filter struct {
	Service   string         // 例: "RDS" / "RDS,EC2"
	Category  string         // 例: "scheduledChange" / "scheduledChange,accountNotification"
	Status    string         // open|upcoming|closed
	Region    string         // 例: "ap-northeast-1" / "us-east-1,us-west-2"
	EventType *regexp.Regexp // eventTypeCode への正規表現
}

// Apply は条件に合致するイベントのみを返す。
func (f Filter) Apply(events []model.Event) []model.Event {
	out := make([]model.Event, 0, len(events))
	for _, e := range events {
		if !matchCSV(f.Service, e.Service) {
			continue
		}
		if !matchCSV(f.Category, e.Category) {
			continue
		}
		if f.Status != "" && !strings.EqualFold(e.StatusCode, f.Status) {
			continue
		}
		if !matchCSV(f.Region, e.Region) {
			continue
		}
		if f.EventType != nil && !f.EventType.MatchString(e.EventTypeCode) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// matchCSV は filter（カンマ区切り・大文字小文字無視）のいずれかに value が一致するか。
// filter が空なら常に true（絞り込みなし）。
func matchCSV(filter, value string) bool {
	if strings.TrimSpace(filter) == "" {
		return true
	}
	for _, f := range strings.Split(filter, ",") {
		if strings.EqualFold(strings.TrimSpace(f), value) {
			return true
		}
	}
	return false
}

// CompileEventType は --event-type の正規表現をコンパイルする（空なら nil）。
func CompileEventType(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid --event-type regex: %w", err)
	}
	return re, nil
}

var relPattern = regexp.MustCompile(`^-?(\d+)([hdw])$`)

// ParseSince は相対表記（-30d, 7d, 2w, 12h）または絶対表記（RFC3339 / 2006-01-02）を
// now を基準に時刻へ変換する。相対表記は常に過去方向。
func ParseSince(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return now, nil
	}
	if m := relPattern.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		var d time.Duration
		switch m[2] {
		case "h":
			d = time.Duration(n) * time.Hour
		case "d":
			d = time.Duration(n) * 24 * time.Hour
		case "w":
			d = time.Duration(n) * 7 * 24 * time.Hour
		}
		return now.Add(-d), nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q (use -30d / 7d / 2w / 12h or RFC3339 / YYYY-MM-DD)", s)
}

// Package group は occurrence（(eventTypeCode,startTime) 単位）を
// eventTypeCode 単位のイベントファミリーへロールアップする最上位の抽象化層。
package group

import (
	"sort"

	"github.com/hacker65536/phd/internal/model"
)

// severity は status の深刻度（小さいほどアクション優先度が高い）。
func severity(s string) int {
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

// ByEventType は論理イベントを eventTypeCode 単位に束ねる。
// region/アカウント/リソースは和、status は件数集計、NextStart は直近の予定開始。
// 並びは「最も深刻な status → 直近開始」で、入力の出現順を初期キーに使う。
func ByEventType(events []model.LogicalEvent) []model.EventGroup {
	order := make([]string, 0)
	byCode := make(map[string]*model.EventGroup)
	regionSet := make(map[string]map[string]bool)
	acctSet := make(map[string]map[string]bool)

	for _, le := range events {
		code := le.EventTypeCode
		g, ok := byCode[code]
		if !ok {
			g = &model.EventGroup{
				EventTypeCode: code,
				Service:       le.Service,
				Category:      le.Category,
				StatusCounts:  map[string]int{},
			}
			byCode[code] = g
			regionSet[code] = map[string]bool{}
			acctSet[code] = map[string]bool{}
			order = append(order, code)
		}
		g.StatusCounts[le.StatusCode]++
		g.OccurrenceCount++
		g.Occurrences = append(g.Occurrences, le)
		for _, r := range le.Regions {
			regionSet[code][r] = true
		}
		for _, res := range le.Resources {
			g.Resources = append(g.Resources, res)
			if res.AccountID != "" {
				acctSet[code][res.AccountID] = true
			}
		}
		if g.Description == "" && le.Description != "" {
			g.Description = le.Description
		}
		// 直近の予定（upcoming のうち最も近い開始）。
		if le.StatusCode == "upcoming" && !le.StartTime.IsZero() {
			if g.NextStart.IsZero() || le.StartTime.Before(g.NextStart) {
				g.NextStart = le.StartTime
				g.NextStatus = "upcoming"
			}
		}
	}

	out := make([]model.EventGroup, 0, len(order))
	for _, code := range order {
		g := byCode[code]
		regions := make([]string, 0, len(regionSet[code]))
		for r := range regionSet[code] {
			regions = append(regions, r)
		}
		sort.Strings(regions)
		g.Regions = regions
		g.AccountCount = len(acctSet[code])
		g.ResourceCount = len(g.Resources)
		// upcoming が無い場合の代表ステータス（最も深刻なもの）。
		if g.NextStatus == "" {
			g.NextStatus = mostSevereStatus(g.StatusCounts)
		}
		out = append(out, *g)
	}

	sort.SliceStable(out, func(i, j int) bool {
		si := severity(mostSevereStatus(out[i].StatusCounts))
		sj := severity(mostSevereStatus(out[j].StatusCounts))
		if si != sj {
			return si < sj
		}
		return out[i].NextStart.Before(out[j].NextStart)
	})
	return out
}

// ByTopic は (eventTypeCode + eventMetadata 署名) 単位に束ねる。
// 同じ話題（例: Lambda Python 3.9 EOL）が region/startTime をまたいで散っていても 1 ファミリーになる。
// metadata が空のイベントは eventTypeCode 単位（= ByEventType 相当）にフォールバックする。
func ByTopic(events []model.LogicalEvent) []model.EventGroup {
	keyed := make([]model.LogicalEvent, len(events))
	copy(keyed, events)
	// キー = code + metadata署名。グルーピング自体は ByEventType と同じ集約ロジックを使うため、
	// 一旦 EventTypeCode を「合成キー」に差し替えてから束ね、表示用に元の値へ戻す。
	type meta struct {
		code  string
		topic string
	}
	info := make(map[string]meta)
	for i := range keyed {
		realCode := keyed[i].EventTypeCode
		sig := topicSignature(keyed[i])
		info[sig] = meta{code: realCode, topic: keyed[i].Topic}
		keyed[i].EventTypeCode = sig
	}
	groups := ByEventType(keyed)
	for i := range groups {
		if m, ok := info[groups[i].EventTypeCode]; ok {
			groups[i].EventTypeCode = m.code
			groups[i].Topic = m.topic
		}
	}
	return groups
}

// topicSignature は code と metadata から束ねキーを作る（metadata 空なら code のみ）。
func topicSignature(le model.LogicalEvent) string {
	if len(le.Metadata) == 0 {
		return le.EventTypeCode
	}
	keys := make([]string, 0, len(le.Metadata))
	for k := range le.Metadata {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sig := le.EventTypeCode
	for _, k := range keys {
		sig += "\x00" + k + "=" + le.Metadata[k]
	}
	return sig
}

func mostSevereStatus(counts map[string]int) string {
	best, bestRank := "", 99
	for s := range counts {
		if r := severity(s); r < bestRank {
			best, bestRank = s, r
		}
	}
	return best
}

package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

// 低カーディナリティの固定 enum 軸（category / status）をチェックボックスで選ぶピッカー。
// 選択は内部的に既存の faceted フィルタのチップ（cat: / st:、カンマ区切り OR）へ落とす。

// categories / statuses は AWS Health の固定 enum（SDK 定義）。
var (
	categories = []string{"issue", "accountNotification", "scheduledChange", "investigation"}
	statuses   = []string{"open", "upcoming", "closed"}
)

// pickerSpec は 1 つのチェックボックス・ピッカーの定義。
type pickerSpec struct {
	field  string // フィルタ軸（"cat" | "st"）
	title  string
	values []string
}

var (
	categoryPicker = pickerSpec{field: "cat", title: "Category フィルタ（space で ON/OFF）", values: categories}
	statusPicker   = pickerSpec{field: "st", title: "Status フィルタ（space で ON/OFF）", values: statuses}
)

// itemCategory は一覧行のカテゴリを返す。
func itemCategory(it list.Item) string {
	switch v := it.(type) {
	case occItem:
		return v.ev.Category
	case groupItem:
		return v.g.Category
	}
	return ""
}

// pickerCounts は最上位フレームの items を軸の値ごとに数える（パネルの件数表示用）。
// status の group 行は複数 status を持ち得るため、各 status へ 1 ずつ加算する。
func (m Model) pickerCounts(field string) map[string]int {
	counts := make(map[string]int)
	for _, it := range m.top().items {
		switch field {
		case "cat":
			counts[itemCategory(it)]++
		case "st":
			switch v := it.(type) {
			case occItem:
				counts[v.ev.StatusCode]++
			case groupItem:
				for k := range v.g.StatusCounts {
					counts[k]++
				}
			}
		}
	}
	return counts
}

// currentSelection は確定済みチップ（field:）から現在の選択状態を復元する。
func currentSelection(field string, values, facets []string) map[string]bool {
	sel := make(map[string]bool)
	for _, f := range facets {
		fld, ok := enumField(f)
		if !ok || fld != field {
			continue
		}
		val := f[strings.Index(f, ":")+1:]
		for _, v := range strings.Split(val, ",") {
			v = strings.TrimSpace(v)
			for _, opt := range values {
				if strings.EqualFold(opt, v) {
					sel[opt] = true
				}
			}
		}
	}
	return sel
}

// removeFacet は指定軸のチップを取り除く。
func removeFacet(facets []string, field string) []string {
	out := make([]string, 0, len(facets))
	for _, x := range facets {
		if f, _ := enumField(x); f != field {
			out = append(out, x)
		}
	}
	return out
}

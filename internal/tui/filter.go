package tui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

// プレフィックス構文付きの一覧フィルタ。bubbles list の Filter として差し替えて使う。
//
// クエリは空白区切りトークンの AND。各トークンは:
//   - svc:RDS / service:RDS   サービス（部分一致・大小無視）
//   - cat:scheduledChange     カテゴリ
//   - st:upcoming / status:   ステータス
//   - reg:ap-northeast-1      リージョン
//   - type:UPGRADE / et:      eventTypeCode（topic ラベル含む）の部分一致
//   - re:^AWS_EC2             eventTypeCode への正規表現（大小無視）
//   - 上記以外のフリーワード    全フィールド横断の部分一致
//
// 例: `svc:RDS cat:scheduledChange`, `re:^AWS_LAMBDA upcoming`, `eks ap-northeast-1`

// fvFields は decodeFV で復元したフィールド群。
type fvFields struct {
	svc, cat, st, typ, reg, all string
}

// decodeFV は encodeFV が作った FilterValue をフィールドへ分解する。
func decodeFV(s string) fvFields {
	p := strings.SplitN(s, fvSep, 5)
	for len(p) < 5 {
		p = append(p, "")
	}
	return fvFields{
		svc: p[0], cat: p[1], st: p[2], typ: p[3], reg: p[4],
		all: strings.Join(p, " "),
	}
}

// filterTok は 1 トークンの照合条件。
type filterTok struct {
	field string // "svc"|"cat"|"st"|"reg"|"type"|"re"|"" (free)
	val   string // 小文字化済み（re は元の値）
	re    *regexp.Regexp
}

// parseQuery はクエリ文字列をトークン列へ分解する。
func parseQuery(q string) []filterTok {
	var toks []filterTok
	for _, raw := range strings.Fields(q) {
		if i := strings.Index(raw, ":"); i > 0 && i < len(raw)-1 {
			key := strings.ToLower(raw[:i])
			val := raw[i+1:]
			switch key {
			case "svc", "service":
				toks = append(toks, filterTok{field: "svc", val: strings.ToLower(val)})
				continue
			case "cat", "category":
				toks = append(toks, filterTok{field: "cat", val: strings.ToLower(val)})
				continue
			case "st", "status":
				toks = append(toks, filterTok{field: "st", val: strings.ToLower(val)})
				continue
			case "reg", "region":
				toks = append(toks, filterTok{field: "reg", val: strings.ToLower(val)})
				continue
			case "type", "et", "event":
				toks = append(toks, filterTok{field: "type", val: strings.ToLower(val)})
				continue
			case "re":
				if rx, err := regexp.Compile("(?i)" + val); err == nil {
					toks = append(toks, filterTok{field: "re", val: val, re: rx})
				} else {
					// 不正な正規表現は eventTypeCode への部分一致にフォールバック。
					toks = append(toks, filterTok{field: "type", val: strings.ToLower(val)})
				}
				continue
			}
		}
		// プレフィックス無し（または未知キー）はフリーワード。
		toks = append(toks, filterTok{field: "", val: strings.ToLower(raw)})
	}
	return toks
}

// match は 1 トークンがレコードに一致するか。
// プレフィックス軸（svc/cat/st/reg/type）は値をカンマ区切りの OR として扱う（例: svc:RDS,LAMBDA）。
func (t filterTok) match(f fvFields) bool {
	switch t.field {
	case "svc":
		return matchCSV(f.svc, t.val)
	case "cat":
		return matchCSV(f.cat, t.val)
	case "st":
		return matchCSV(f.st, t.val)
	case "reg":
		return matchCSV(f.reg, t.val)
	case "type":
		return matchCSV(f.typ, t.val)
	case "re":
		return t.re.MatchString(f.typ)
	default:
		return strings.Contains(strings.ToLower(f.all), t.val)
	}
}

// matchCSV は field がカンマ区切り val のいずれか（OR）を部分一致で含むか。
// val は小文字化済み。カンマを含まなければ単なる部分一致と同じ。
func matchCSV(field, val string) bool {
	fl := strings.ToLower(field)
	for _, v := range strings.Split(val, ",") {
		if v = strings.TrimSpace(v); v != "" && strings.Contains(fl, v) {
			return true
		}
	}
	return false
}

// enumField は raw が低カーディナリティ軸トークン（svc:/cat:/st:/reg:）なら正規フィールド名を返す。
// type:/et:/re: や bare ワードは自由語扱いとして ok=false。
func enumField(raw string) (string, bool) {
	i := strings.Index(raw, ":")
	if i <= 0 || i >= len(raw)-1 {
		return "", false
	}
	switch strings.ToLower(raw[:i]) {
	case "svc", "service":
		return "svc", true
	case "cat", "category":
		return "cat", true
	case "st", "status":
		return "st", true
	case "reg", "region":
		return "reg", true
	}
	return "", false
}

// splitQuery は入力クエリを「enum チップ（正規化済み）」と「自由語の残り」に分ける。
func splitQuery(q string) (chips []string, free string) {
	var frees []string
	for _, raw := range strings.Fields(q) {
		if f, ok := enumField(raw); ok {
			val := raw[strings.Index(raw, ":")+1:]
			chips = append(chips, f+":"+val) // キーを正規化（service→svc 等）
		} else {
			frees = append(frees, raw)
		}
	}
	return chips, strings.Join(frees, " ")
}

// upsertFacet は同一軸のチップを置き換えて追加する（enum は 1 軸 1 値=単一選択）。
func upsertFacet(facets []string, tok string) []string {
	f, _ := enumField(tok)
	out := make([]string, 0, len(facets)+1)
	for _, x := range facets {
		if xf, _ := enumField(x); xf != f {
			out = append(out, x)
		}
	}
	return append(out, tok)
}

// filterItems は items に query を適用した部分集合を返す（query 空なら全件、入力順を保持）。
func filterItems(query string, items []list.Item) []list.Item {
	if strings.TrimSpace(query) == "" {
		return items
	}
	targets := make([]string, len(items))
	for i, it := range items {
		targets[i] = it.FilterValue()
	}
	ranks := rankFilter(query, targets)
	out := make([]list.Item, 0, len(ranks))
	for _, r := range ranks {
		out = append(out, items[r.Index])
	}
	return out
}

// rankFilter は query をプレフィックス構文で評価し、一致した target の Rank を入力順で返す。
func rankFilter(term string, targets []string) []list.Rank {
	toks := parseQuery(term)
	if len(toks) == 0 {
		out := make([]list.Rank, len(targets))
		for i := range targets {
			out[i] = list.Rank{Index: i}
		}
		return out
	}
	var out []list.Rank
	for i, tgt := range targets {
		f := decodeFV(tgt)
		ok := true
		for _, t := range toks {
			if !t.match(f) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, list.Rank{Index: i})
		}
	}
	return out
}

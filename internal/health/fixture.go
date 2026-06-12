package health

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/hacker65536/phd/internal/model"
)

// Fixture は AWS を一切呼ばずにデモ表示を再現するためのデータ束。
// 録画・スクリーンショット用に、実データを scrub（匿名化）して書き出したものを想定する。
// 取得 4 経路（events / details / resources / affected-accounts）と
// アカウント名解決を、ARN をキーに丸ごと差し替える。
type Fixture struct {
	CapturedAt time.Time                   `json:"capturedAt"`       // 取得時刻（--demo 時に now 基準へリベース）
	Events     []model.Event               `json:"events"`           // 生イベント（merge 前）
	Details    map[string]Detail           `json:"details"`          // arn -> 説明・eventMetadata
	Resources  map[string][]model.Resource `json:"resources"`        // arn -> 影響リソース
	Affected   map[string][]string         `json:"affectedAccounts"` // arn -> 影響アカウント ID
	Accounts   map[string]string           `json:"accountNames"`     // id -> 名前
}

// LoadFixture は JSON ファイルから Fixture を読み込む。
func LoadFixture(path string) (*Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fx Fixture
	if err := json.Unmarshal(data, &fx); err != nil {
		return nil, fmt.Errorf("parse fixture %q: %w", path, err)
	}
	return &fx, nil
}

// Save は Fixture を整形 JSON で書き出す。
func (f *Fixture) Save(path string) error {
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// NewFixture は Fixture を供給するオフライン用 Client を返す（AWS API は持たない）。
// 4 つの取得メソッドは fixture から応答するため、cfg / cache / limiter は不要。
func NewFixture(ns string, fx *Fixture) *Client {
	return &Client{ns: ns, fixture: fx}
}

// RebaseFixture は全イベントの時刻を now - CapturedAt だけずらし、録画時期がずれても
// open/upcoming の鮮度（PruneStaleOpen / ApplyHorizon が now 基準）が保たれるようにする。
// CapturedAt がゼロなら何もしない。
func RebaseFixture(fx *Fixture, now time.Time) {
	if fx == nil || fx.CapturedAt.IsZero() {
		return
	}
	shift := now.Sub(fx.CapturedAt)
	add := func(t time.Time) time.Time {
		if t.IsZero() {
			return t
		}
		return t.Add(shift)
	}
	for i := range fx.Events {
		fx.Events[i].StartTime = add(fx.Events[i].StartTime)
		fx.Events[i].EndTime = add(fx.Events[i].EndTime)
		fx.Events[i].LastUpdated = add(fx.Events[i].LastUpdated)
	}
	fx.CapturedAt = now
}

// accountsFromResources は Resources から影響アカウント ID を一意・整列して導く
// （Affected 未登録の ARN のフォールバック）。
func accountsFromResources(res []model.Resource) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, r := range res {
		if r.AccountID == "" || seen[r.AccountID] {
			continue
		}
		seen[r.AccountID] = true
		ids = append(ids, r.AccountID)
	}
	sort.Strings(ids)
	return ids
}

// BuildFixture は実 Client で全イベントの詳細・影響リソース・影響アカウントを取得し、
// Fixture を組み立てる（--dump-fixture 用）。取得は既存のキャッシュ・レート制御を共有する。
// names は ID→名前 マップ（org 以外や解決失敗時は空でよい）。
func BuildFixture(ctx context.Context, c *Client, org bool, events []model.Event, names map[string]string) (*Fixture, error) {
	if names == nil {
		names = map[string]string{}
	}
	fx := &Fixture{
		CapturedAt: time.Now().UTC(),
		Events:     events,
		Details:    make(map[string]Detail),
		Resources:  make(map[string][]model.Resource),
		Affected:   make(map[string][]string),
		Accounts:   names,
	}
	seen := make(map[string]bool)
	for _, e := range events {
		if e.Arn == "" || seen[e.Arn] {
			continue
		}
		seen[e.Arn] = true

		d, err := c.FetchDetails(ctx, org, e.Arn)
		if err != nil {
			return nil, fmt.Errorf("fetch details %s: %w", e.Arn, err)
		}
		fx.Details[e.Arn] = d

		res, err := c.FetchResources(ctx, org, e.Arn, e.Region)
		if err != nil {
			return nil, fmt.Errorf("fetch resources %s: %w", e.Arn, err)
		}
		fx.Resources[e.Arn] = res

		if org {
			if ids, err := c.fetchAffectedAccountsOrg(ctx, e.Arn); err == nil {
				fx.Affected[e.Arn] = ids
			}
		}
	}
	return fx, nil
}

var twelveDigit = regexp.MustCompile(`\d{12}`)

// Scrub は PII（アカウント ID・リソース値・アカウント名・任意トークン）を決定論的に匿名化する。
// eventTypeCode / service / region / 時系列は保持する（リアルさを残すため）。
// repl は追加の固定置換（会社名 → プレースホルダ等、old→new）。
func (f *Fixture) Scrub(repl map[string]string) {
	idMap := map[string]string{}
	valMap := map[string]string{}
	nameMap := map[string]string{}
	var idN, valN, nameN int

	addID := func(s string) {
		if s == "" {
			return
		}
		if _, ok := idMap[s]; !ok {
			idN++
			idMap[s] = fmt.Sprintf("%012d", 100000000000+idN)
		}
	}
	scanIDs := func(s string) {
		for _, m := range twelveDigit.FindAllString(s, -1) {
			addID(m)
		}
	}
	addVal := func(s string) {
		if s == "" {
			return
		}
		if _, ok := valMap[s]; !ok {
			valN++
			valMap[s] = scrubValue(s, valN)
		}
	}
	addName := func(s string) {
		if s == "" {
			return
		}
		if _, ok := nameMap[s]; !ok {
			nameN++
			nameMap[s] = fmt.Sprintf("Demo Account %02d", nameN)
		}
	}

	// 決定論的な走査順でマッピングを採番する。
	for _, e := range f.Events {
		scanIDs(e.Arn)
		if d, ok := f.Details[e.Arn]; ok {
			scanIDs(d.Description)
			for _, k := range sortedKeys(d.Metadata) {
				scanIDs(d.Metadata[k])
			}
		}
		for _, r := range f.Resources[e.Arn] {
			addID(r.AccountID)
			scanIDs(r.Value)
			addVal(r.Value)
			addName(r.AccountName)
		}
		for _, id := range f.Affected[e.Arn] {
			addID(id)
		}
	}
	for _, id := range sortedKeys(f.Accounts) {
		addID(id)
		addName(f.Accounts[id])
	}

	// 自由文（ARN・説明・metadata 値）への部分置換用。長いキー優先で衝突を避ける。
	rw := buildReplacer(repl, valMap, nameMap, idMap)
	mapOr := func(m map[string]string, s string) string {
		if v, ok := m[s]; ok {
			return v
		}
		return s
	}

	for i := range f.Events {
		f.Events[i].Arn = rw.Replace(f.Events[i].Arn)
	}
	newDetails := make(map[string]Detail, len(f.Details))
	for arn, d := range f.Details {
		d.Description = rw.Replace(d.Description)
		if d.Metadata != nil {
			md := make(map[string]string, len(d.Metadata))
			for k, v := range d.Metadata {
				md[k] = rw.Replace(v)
			}
			d.Metadata = md
		}
		newDetails[rw.Replace(arn)] = d
	}
	newResources := make(map[string][]model.Resource, len(f.Resources))
	for arn, rs := range f.Resources {
		out := make([]model.Resource, len(rs))
		for i, r := range rs {
			r.AccountID = mapOr(idMap, r.AccountID)
			r.AccountName = mapOr(nameMap, r.AccountName)
			r.Value = mapOr(valMap, r.Value)
			out[i] = r
		}
		newResources[rw.Replace(arn)] = out
	}
	newAffected := make(map[string][]string, len(f.Affected))
	for arn, ids := range f.Affected {
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = mapOr(idMap, id)
		}
		newAffected[rw.Replace(arn)] = out
	}
	newAccounts := make(map[string]string, len(f.Accounts))
	for id, name := range f.Accounts {
		newAccounts[mapOr(idMap, id)] = mapOr(nameMap, name)
	}
	f.Details = newDetails
	f.Resources = newResources
	f.Affected = newAffected
	f.Accounts = newAccounts
}

// scrubValue はリソース識別子をプレフィックス保持で連番化する。
func scrubValue(old string, n int) string {
	switch {
	case strings.HasPrefix(old, "i-"):
		return fmt.Sprintf("i-0demo%06d", n)
	case strings.HasPrefix(old, "vol-"):
		return fmt.Sprintf("vol-0demo%06d", n)
	case strings.HasPrefix(old, "arn:"):
		return fmt.Sprintf("arn:aws:demo:::resource/%03d", n)
	default:
		return fmt.Sprintf("demo-res-%03d", n)
	}
}

// buildReplacer は全マッピングを長いキー優先で 1 つの Replacer にまとめる。
func buildReplacer(maps ...map[string]string) *strings.Replacer {
	type pair struct{ old, new string }
	var pairs []pair
	for _, m := range maps {
		for o, n := range m {
			if o != "" {
				pairs = append(pairs, pair{o, n})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if len(pairs[i].old) != len(pairs[j].old) {
			return len(pairs[i].old) > len(pairs[j].old) // 長いキー優先
		}
		return pairs[i].old < pairs[j].old
	})
	flat := make([]string, 0, len(pairs)*2)
	for _, p := range pairs {
		flat = append(flat, p.old, p.new)
	}
	return strings.NewReplacer(flat...)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"phd/internal/health"
	"phd/internal/model"
)

func sampleOccs() []model.LogicalEvent {
	return []model.LogicalEvent{
		{
			EventTypeCode: "AWS_EC2_RETIREMENT",
			Service:       "EC2",
			Category:      "scheduledChange",
			StatusCode:    "upcoming",
			Regions:       []string{"ap-northeast-1", "us-east-1"},
			StartTime:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
			RawEvents: []model.Event{
				{Arn: "arn:ec2:1", Region: "ap-northeast-1"},
				{Arn: "arn:ec2:2", Region: "us-east-1"},
			},
		},
		{
			EventTypeCode: "AWS_RDS_MAINTENANCE",
			Service:       "RDS",
			Category:      "scheduledChange",
			StatusCode:    "open",
			Regions:       []string{"us-east-1"},
			StartTime:     time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
			RawEvents:     []model.Event{{Arn: "arn:rds:1", Region: "us-east-1"}},
		},
	}
}

func newTestModel(events []model.LogicalEvent, groupBy string) Model {
	return New(context.Background(), &Input{
		Org:     true,
		Events:  events,
		Now:     time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC),
		GroupBy: groupBy,
	})
}

// update は Update を呼んで Model に型アサートして返すヘルパー。
func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	mm, ok := next.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", next)
	}
	return mm, cmd
}

func key(s tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: s} }

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// startFilter は "/" で絞り込みを開始し query を入力する（未確定 = Filtering 中）。
func startFilter(t *testing.T, m Model, query string) Model {
	t.Helper()
	m, _ = update(t, m, runes("/"))
	if !m.filtering {
		t.Fatalf("after '/', filtering = false, want true")
	}
	m, _ = update(t, m, runes(query))
	return m
}

// applyFilter は "/" → 入力 → Enter確定 まで実行し、確定フィルタを持つ Model を返す。
func applyFilter(t *testing.T, m Model, query string) Model {
	t.Helper()
	m = startFilter(t, m, query)
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.filtering {
		t.Fatalf("after enter, still filtering")
	}
	return m
}

// TestFilterAppliesLive は入力に追従して同期的に絞り込まれることを検証する（自前フィルタ）。
func TestFilterAppliesLive(t *testing.T) {
	m := newTestModel(sampleOccs(), "") // EC2(index0) と RDS(index1) の 2 件
	if got := len(m.list.VisibleItems()); got != 2 {
		t.Fatalf("initial visible = %d, want 2", got)
	}
	m = startFilter(t, m, "svc:RDS")
	vis := m.list.VisibleItems()
	if len(vis) != 1 {
		t.Fatalf("live-filtered visible = %d, want 1", len(vis))
	}
	if oi, ok := vis[0].(occItem); !ok || oi.ev.Service != "RDS" {
		t.Fatalf("filtered item = %+v, want RDS occItem", vis[0])
	}
}

// TestEnumBecomesChipNotInInput は enum 軸がチップ（status line）になり入力欄には残らないことを検証する。
func TestEnumBecomesChipNotInInput(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m = applyFilter(t, m, "svc:RDS")

	if got := m.top().facets; len(got) != 1 || got[0] != "svc:RDS" {
		t.Fatalf("facets = %v, want [svc:RDS]", got)
	}
	if got := m.top().free; got != "" {
		t.Fatalf("free = %q, want empty", got)
	}
	if got := m.fi.Value(); got != "" {
		t.Fatalf("input value = %q, want empty (enum should not stay in input)", got)
	}
	if s := m.chipLine(); !strings.Contains(s, "svc:RDS") {
		t.Fatalf("chip line = %q, want to contain svc:RDS", s)
	}
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible = %d, want 1", got)
	}
}

// TestFreeWordRetainedInInput は自由語が入力欄に残り、再オープンで復元されることを検証する。
func TestFreeWordRetainedInInput(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m = applyFilter(t, m, "retirement") // EC2 の eventTypeCode に一致

	if got := m.top().free; got != "retirement" {
		t.Fatalf("free = %q, want retirement", got)
	}
	if len(m.top().facets) != 0 {
		t.Fatalf("facets = %v, want empty", m.top().facets)
	}
	if got := m.fi.Value(); got != "retirement" {
		t.Fatalf("input value = %q, want retirement (free word retained in input)", got)
	}
	// 再度 "/" → 自由語が入力欄に復元されている。
	m, _ = update(t, m, runes("/"))
	if got := m.fi.Value(); got != "retirement" {
		t.Fatalf("reopened input = %q, want retirement (retained)", got)
	}
}

// TestMixedCommitSplits は enum とフリーワードが混在した入力が正しく分離されることを検証する。
func TestMixedCommitSplits(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m = applyFilter(t, m, "svc:EC2 retirement") // EC2 かつ eventTypeCode に retirement

	if got := m.top().facets; len(got) != 1 || got[0] != "svc:EC2" {
		t.Fatalf("facets = %v, want [svc:EC2]", got)
	}
	if got := m.top().free; got != "retirement" {
		t.Fatalf("free = %q, want retirement", got)
	}
	if got := m.fi.Value(); got != "retirement" {
		t.Fatalf("input value = %q, want retirement", got)
	}
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible = %d, want 1 (EC2 ∧ retirement)", got)
	}
}

func TestFilterPersistsAcrossDrillDown(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m = applyFilter(t, m, "svc:RDS")
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("applied visible = %d, want 1", got)
	}

	// 絞り込んだ1件にドリルダウン → 戻る。
	m, _ = update(t, m, key(tea.KeyEnter)) // → detail (RDS)
	if m.top().level != levelDetail {
		t.Fatalf("expected detail level after drill-down, got %d", m.top().level)
	}
	m, _ = update(t, m, key(tea.KeyEsc)) // → 戻る

	// チップが維持されていること。
	if got := m.top().facets; len(got) != 1 || got[0] != "svc:RDS" {
		t.Fatalf("after back, facets = %v, want [svc:RDS] (should persist)", got)
	}
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("after back, visible = %d, want 1 (filter should persist)", got)
	}
}

// TestEscClearsFilterThenGoesBack は Esc がまずフィルタ解除、次に階層を戻る2段階であることを検証する。
func TestEscClearsFilterThenGoesBack(t *testing.T) {
	m := newTestModel(sampleOccs(), "type") // group ルート
	m, _ = update(t, m, key(tea.KeyEnter))  // group → occ 階層へ
	m = applyFilter(t, m, "svc:RDS")
	depth := len(m.stack)

	// 1回目の Esc: フィルタ解除（階層は維持）。
	m, _ = update(t, m, key(tea.KeyEsc))
	if len(m.top().facets) != 0 || m.top().free != "" {
		t.Fatalf("after 1st esc, facets=%v free=%q, want cleared", m.top().facets, m.top().free)
	}
	if len(m.stack) != depth {
		t.Fatalf("1st esc changed depth %d -> %d (should only clear filter)", depth, len(m.stack))
	}
	// 2回目の Esc: 1階層戻る。
	m, _ = update(t, m, key(tea.KeyEsc))
	if len(m.stack) != depth-1 {
		t.Fatalf("2nd esc depth = %d, want %d (go back)", len(m.stack), depth-1)
	}
}

func TestSplitQuery(t *testing.T) {
	chips, free := splitQuery("svc:RDS cat:scheduledChange lambda re:^AWS type:UPG")
	wantChips := []string{"svc:RDS", "cat:scheduledChange"}
	if !equalStrs(chips, wantChips) {
		t.Fatalf("chips = %v, want %v", chips, wantChips)
	}
	if free != "lambda re:^AWS type:UPG" {
		t.Fatalf("free = %q, want 'lambda re:^AWS type:UPG'", free)
	}
	// service→svc 正規化。
	if c, _ := splitQuery("service:RDS"); len(c) != 1 || c[0] != "svc:RDS" {
		t.Fatalf("alias normalize = %v, want [svc:RDS]", c)
	}
}

// TestUpsertFacetReplacesSameAxis は同一軸のチップが置き換えられる（単一選択）ことを検証する。
func TestUpsertFacetReplacesSameAxis(t *testing.T) {
	f := upsertFacet([]string{"svc:RDS", "cat:issue"}, "svc:EC2")
	if !equalStrs(f, []string{"cat:issue", "svc:EC2"}) {
		t.Fatalf("upsert = %v, want [cat:issue svc:EC2]", f)
	}
}

func equalStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDrillDownAndBack(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	if m.top().level != levelOccs {
		t.Fatalf("root level = %d, want levelOccs", m.top().level)
	}

	// Enter → 詳細へ。遅延ロード発火（state=loading）。
	m, cmd := update(t, m, key(tea.KeyEnter))
	if m.top().level != levelDetail {
		t.Fatalf("after enter level = %d, want levelDetail", m.top().level)
	}
	if cmd == nil {
		t.Fatal("expected a load command on first drill-down")
	}
	st := m.state["arn:ec2:1"]
	if st == nil || st.dState != stateLoading || st.rState != stateLoading {
		t.Fatalf("expected loading state, got %+v", st)
	}

	// Esc → 戻る。
	m, _ = update(t, m, key(tea.KeyEsc))
	if m.top().level != levelOccs {
		t.Fatalf("after esc level = %d, want levelOccs", m.top().level)
	}

	// 最上位での Esc は no-op。
	before := len(m.stack)
	m, _ = update(t, m, key(tea.KeyEsc))
	if len(m.stack) != before {
		t.Fatalf("esc at root changed stack depth: %d -> %d", before, len(m.stack))
	}
}

func TestLazyLoadStateMachine(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter)) // → detail (arn:ec2:1)

	// 到着メッセージ注入 → loaded。
	m, _ = update(t, m, detailLoadedMsg{key: "arn:ec2:1", detail: health.Detail{Description: "boom"}})
	m, _ = update(t, m, resLoadedMsg{key: "arn:ec2:1", resources: []model.Resource{
		{AccountID: "111", Region: "us-east-1", Value: "i-abc", Status: "PENDING"},
	}})
	st := m.state["arn:ec2:1"]
	if st.dState != stateLoaded || st.rState != stateLoaded {
		t.Fatalf("after load msgs, state = %+v", st)
	}
	if st.detail.Description != "boom" || len(st.resources) != 1 {
		t.Fatalf("loaded data not stored: %+v", st)
	}

	// 戻って再度入ると、ロード済みなので新たな Cmd を返さない。
	m, _ = update(t, m, key(tea.KeyEsc))
	m, cmd := update(t, m, key(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("re-entering a loaded occurrence should not issue load commands")
	}
}

func TestLoadErrorMarksFailed(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter))
	m, _ = update(t, m, loadErrMsg{key: "arn:ec2:1", kind: "detail", err: errFake{}})
	if st := m.state["arn:ec2:1"]; st.dState != stateFailed {
		t.Fatalf("detail err: dState = %d, want failed", st.dState)
	}
}

type errFake struct{}

func (errFake) Error() string { return "fake" }

func TestAccountNamesAppliedRetroactively(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	m, _ = update(t, m, key(tea.KeyEnter))
	// 先にリソースが届く（名前マップはまだ無い）。
	m, _ = update(t, m, resLoadedMsg{key: "arn:ec2:1", resources: []model.Resource{
		{AccountID: "111", Region: "us-east-1", Value: "i-abc"},
	}})
	if m.state["arn:ec2:1"].resources[0].AccountName != "" {
		t.Fatal("name should be empty before accounts arrive")
	}
	// 後からアカウント名マップが届く → 遡及適用。
	m, _ = update(t, m, accountsLoadedMsg{names: map[string]string{"111": "acct-a"}})
	if got := m.state["arn:ec2:1"].resources[0].AccountName; got != "acct-a" {
		t.Fatalf("retroactive name = %q, want acct-a", got)
	}
}

func TestGroupRootDrillsToOccurrences(t *testing.T) {
	m := newTestModel(sampleOccs(), "type")
	if m.top().level != levelGroups {
		t.Fatalf("root level = %d, want levelGroups", m.top().level)
	}
	// group → occurrences → detail の2段ドリルダウン。
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.top().level != levelOccs {
		t.Fatalf("after 1st enter level = %d, want levelOccs", m.top().level)
	}
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.top().level != levelDetail {
		t.Fatalf("after 2nd enter level = %d, want levelDetail", m.top().level)
	}
}

func catOccs() []model.LogicalEvent {
	return []model.LogicalEvent{
		{EventTypeCode: "AWS_EC2_X", Service: "EC2", Category: "issue", StatusCode: "open", RawEvents: []model.Event{{Arn: "a1"}}},
		{EventTypeCode: "AWS_RDS_Y", Service: "RDS", Category: "scheduledChange", StatusCode: "open", RawEvents: []model.Event{{Arn: "a2"}}},
		{EventTypeCode: "AWS_S3_Z", Service: "S3", Category: "accountNotification", StatusCode: "open", RawEvents: []model.Event{{Arn: "a3"}}},
	}
}

func hasFacet(facets []string, want string) bool {
	for _, f := range facets {
		if f == want {
			return true
		}
	}
	return false
}

func TestCategoryPickerSingle(t *testing.T) {
	m := newTestModel(catOccs(), "")
	if got := len(m.list.VisibleItems()); got != 3 {
		t.Fatalf("initial visible = %d, want 3", got)
	}
	// "c" でパネルを開く（cursor は categories[0]="issue"）。
	m, _ = update(t, m, runes("c"))
	if m.picker == nil || m.picker.field != "cat" {
		t.Fatal("c should open category picker")
	}
	// space で issue を ON → enter で適用。
	m, _ = update(t, m, runes(" "))
	if !m.pickerSel["issue"] {
		t.Fatal("space should toggle issue on")
	}
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.picker != nil {
		t.Fatal("enter should close picker")
	}
	if !hasFacet(m.top().facets, "cat:issue") {
		t.Fatalf("facets = %v, want to contain cat:issue", m.top().facets)
	}
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible = %d, want 1 (issue only)", got)
	}
	// 再オープンで選択状態が復元される。
	m, _ = update(t, m, runes("c"))
	if !m.pickerSel["issue"] {
		t.Fatal("reopen should restore issue selection from cat: chip")
	}
}

func TestCategoryPickerMultiAndClear(t *testing.T) {
	m := newTestModel(catOccs(), "")
	m, _ = update(t, m, runes("c"))
	// issue(0) を ON、down×2 で scheduledChange(2) を ON。
	m, _ = update(t, m, runes(" "))
	m, _ = update(t, m, key(tea.KeyDown))
	m, _ = update(t, m, key(tea.KeyDown))
	m, _ = update(t, m, runes(" "))
	m, _ = update(t, m, key(tea.KeyEnter))
	if !hasFacet(m.top().facets, "cat:issue,scheduledChange") {
		t.Fatalf("facets = %v, want cat:issue,scheduledChange", m.top().facets)
	}
	if got := len(m.list.VisibleItems()); got != 2 {
		t.Fatalf("visible = %d, want 2 (issue + scheduledChange)", got)
	}
	// 全 OFF にして適用 → cat: チップが消えて全件に戻る。
	m, _ = update(t, m, runes("c"))
	m, _ = update(t, m, runes(" ")) // issue off
	m, _ = update(t, m, key(tea.KeyDown))
	m, _ = update(t, m, key(tea.KeyDown))
	m, _ = update(t, m, runes(" ")) // scheduledChange off
	m, _ = update(t, m, key(tea.KeyEnter))
	for _, f := range m.top().facets {
		if len(f) >= 4 && f[:4] == "cat:" {
			t.Fatalf("cat facet should be removed, got %v", m.top().facets)
		}
	}
	if got := len(m.list.VisibleItems()); got != 3 {
		t.Fatalf("visible = %d, want 3 (cleared)", got)
	}
}

func TestCatStatusInStatusLineNotChips(t *testing.T) {
	m := newTestModel(catOccs(), "")
	// svc は自由入力 → チップ、cat はピッカー → status line。
	m = applyFilter(t, m, "svc:EC2")
	m, _ = update(t, m, runes("c"))
	m, _ = update(t, m, runes(" ")) // issue on
	m, _ = update(t, m, key(tea.KeyEnter))

	// svc はチップ行に出る。
	if got := m.chipLine(); !strings.Contains(got, "svc:EC2") {
		t.Fatalf("chip line = %q, want svc:EC2", got)
	}
	// cat はチップ行に出ない。
	if got := m.chipLine(); strings.Contains(got, "cat") {
		t.Fatalf("chip line should not contain cat: %q", got)
	}
	// cat は下部 status line に出る。
	if !m.catStatusShown() {
		t.Fatal("catStatusShown should be true")
	}
	if got := m.catStatusLine(); !strings.Contains(got, "category: issue") {
		t.Fatalf("status line = %q, want 'category: issue'", got)
	}
}

func TestStatusPicker(t *testing.T) {
	// open(1) + upcoming(1) + closed(1) の 3 件。
	evs := []model.LogicalEvent{
		{EventTypeCode: "A", Service: "EC2", Category: "issue", StatusCode: "open", RawEvents: []model.Event{{Arn: "a1"}}},
		{EventTypeCode: "B", Service: "RDS", Category: "issue", StatusCode: "upcoming", RawEvents: []model.Event{{Arn: "a2"}}},
		{EventTypeCode: "C", Service: "S3", Category: "issue", StatusCode: "closed", RawEvents: []model.Event{{Arn: "a3"}}},
	}
	m := newTestModel(evs, "")
	// "s" で status ピッカー（statuses[0]="open"）。
	m, _ = update(t, m, runes("s"))
	if m.picker == nil || m.picker.field != "st" {
		t.Fatal("s should open status picker")
	}
	// open を ON → enter。
	m, _ = update(t, m, runes(" "))
	m, _ = update(t, m, key(tea.KeyEnter))
	if !hasFacet(m.top().facets, "st:open") {
		t.Fatalf("facets = %v, want st:open", m.top().facets)
	}
	if got := len(m.list.VisibleItems()); got != 1 {
		t.Fatalf("visible = %d, want 1 (open only)", got)
	}
	// category と status は別軸なので共存できる。
	m, _ = update(t, m, runes("c"))
	m, _ = update(t, m, runes(" ")) // issue on
	m, _ = update(t, m, key(tea.KeyEnter))
	if !hasFacet(m.top().facets, "st:open") || !hasFacet(m.top().facets, "cat:issue") {
		t.Fatalf("facets = %v, want both st:open and cat:issue", m.top().facets)
	}
}

func TestEOLLabelAndFilter(t *testing.T) {
	evs := []model.LogicalEvent{
		{EventTypeCode: "AWS_LAMBDA_PLANNED_LIFECYCLE_EVENT", Service: "LAMBDA", Category: "scheduledChange", StatusCode: "open", RawEvents: []model.Event{{Arn: "a1"}}},
		{EventTypeCode: "AWS_RDS_MAINTENANCE", Service: "RDS", Category: "scheduledChange", StatusCode: "open", RawEvents: []model.Event{{Arn: "a2"}}},
	}
	// Title に [EOL] が付くのは lifecycle 系だけ。
	if got := (occItem{ev: evs[0]}).Title(); !strings.Contains(got, "[EOL]") {
		t.Fatalf("lifecycle title = %q, want [EOL]", got)
	}
	if got := (occItem{ev: evs[1]}).Title(); strings.Contains(got, "[EOL]") {
		t.Fatalf("non-lifecycle title = %q, should not contain [EOL]", got)
	}
	// `eol` フリーワードで lifecycle 系だけ抽出できる。
	m := newTestModel(evs, "")
	m = applyFilter(t, m, "eol")
	vis := m.list.VisibleItems()
	if len(vis) != 1 {
		t.Fatalf("filter eol visible = %d, want 1", len(vis))
	}
	if oi, ok := vis[0].(occItem); !ok || oi.ev.Service != "LAMBDA" {
		t.Fatalf("filtered = %+v, want LAMBDA lifecycle", vis[0])
	}
}

func TestTitleCountAggregation(t *testing.T) {
	m := newTestModel(catOccs(), "") // 3 件
	if got := m.list.Title; got != "Health events — 3 occurrence(s)" {
		t.Fatalf("title = %q, want 'Health events — 3 occurrence(s)'", got)
	}
	// 絞り込むと M/N 表記。
	m = applyFilter(t, m, "svc:RDS")
	if got := m.list.Title; got != "Health events — 1/3 occurrence(s)" {
		t.Fatalf("filtered title = %q, want 'Health events — 1/3 occurrence(s)'", got)
	}
	// チップ行に件数は出さない（Title に集約）。
	if s := m.chipLine(); strings.Contains(s, "1/3") || strings.Contains(s, "3/3") {
		t.Fatalf("chip line should not contain count: %q", s)
	}
}

func TestResourcesPageNavigation(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	// 一覧 → 詳細(level 2)。
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.top().level != levelDetail {
		t.Fatalf("after 1st enter level = %d, want levelDetail", m.top().level)
	}
	// リソースが届いたとする。
	m, _ = update(t, m, resLoadedMsg{key: "arn:ec2:1", resources: []model.Resource{
		{AccountID: "111", Region: "us-east-1", Value: "i-abc", Status: "PENDING"},
	}})
	// 詳細(level 2)では影響リソースの「表」は出さない（件数ヒントのみ）。
	dc := m.detailContent(m.top().occ, m.state["arn:ec2:1"])
	if strings.Contains(dc, "ACCOUNT\tREGION") || strings.Contains(dc, "i-abc") {
		t.Fatalf("detail page should not contain the resource table: %q", dc)
	}
	if !strings.Contains(dc, "Affected resources:") {
		t.Fatalf("detail page should show resources hint: %q", dc)
	}
	// 詳細 → 影響リソース一覧(level 3)。
	m, _ = update(t, m, key(tea.KeyEnter))
	if m.top().level != levelResources {
		t.Fatalf("after 2nd enter level = %d, want levelResources", m.top().level)
	}
	rc := m.resourcesContent(m.state["arn:ec2:1"])
	if !strings.Contains(rc, "i-abc") {
		t.Fatalf("resources page should list the resource: %q", rc)
	}
	// 戻ると詳細(level 2)。
	m, _ = update(t, m, key(tea.KeyEsc))
	if m.top().level != levelDetail {
		t.Fatalf("after esc level = %d, want levelDetail", m.top().level)
	}
	// もう一度戻ると一覧。
	m, _ = update(t, m, key(tea.KeyEsc))
	if m.top().level != levelOccs {
		t.Fatalf("after 2nd esc level = %d, want levelOccs", m.top().level)
	}
}

func TestResourcesSortHideToggle(t *testing.T) {
	st := &occState{rState: stateLoaded, resources: []model.Resource{
		{AccountID: "333", AccountName: "charlie", Region: "us-east-1", Value: "r-c", Status: "IMPAIRED"},
		{AccountID: "111", AccountName: "alice", Region: "us-east-1", Value: "r-a", Status: "RESOLVED"},
		{AccountID: "222", AccountName: "bob", Region: "ap-northeast-1", Value: "r-b", Status: "PENDING"},
	}}

	// ① アカウント順ソート（alice→bob→charlie）。
	sorted := sortResourcesByAccount(st.resources)
	got := []string{sorted[0].AccountName, sorted[1].AccountName, sorted[2].AccountName}
	if got[0] != "alice" || got[1] != "bob" || got[2] != "charlie" {
		t.Fatalf("sort order = %v, want [alice bob charlie]", got)
	}

	m := newTestModel(sampleOccs(), "")
	// ② 既定では RESOLVED を非表示（alice の r-a が消える）。
	if m.showResolved {
		t.Fatal("showResolved should default to false")
	}
	vis := m.visibleResources(st)
	if len(vis) != 2 {
		t.Fatalf("default visible = %d, want 2 (RESOLVED hidden)", len(vis))
	}
	rc := m.resourcesContent(st)
	if strings.Contains(rc, "r-a") {
		t.Fatalf("default should hide RESOLVED resource r-a: %q", rc)
	}
	if !strings.Contains(rc, "非表示中") {
		t.Fatalf("should note hidden count: %q", rc)
	}

	// ③ a トグルで全表示（RESOLVED も出る）。
	m, _ = update(t, m, key(tea.KeyEnter)) // → detail
	m, _ = update(t, m, resLoadedMsg{key: "arn:ec2:1", resources: st.resources})
	m, _ = update(t, m, key(tea.KeyEnter)) // → resources page
	if m.top().level != levelResources {
		t.Fatalf("expected resources level, got %d", m.top().level)
	}
	if got := len(m.visibleResources(m.state["arn:ec2:1"])); got != 2 {
		t.Fatalf("resources page default visible = %d, want 2", got)
	}
	m, _ = update(t, m, runes("a")) // 全表示
	if !m.showResolved {
		t.Fatal("'a' should toggle showResolved on")
	}
	if got := len(m.visibleResources(m.state["arn:ec2:1"])); got != 3 {
		t.Fatalf("after 'a' visible = %d, want 3 (all)", got)
	}
}

func TestDetailSubtitleShowsTopic(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	occ := sampleOccs()[0]
	st := &occState{dState: stateLoaded, detail: health.Detail{
		Description: "desc",
		Metadata:    map[string]string{"deprecated_versions": "AWS Lambda end of support for Python 3.9"},
	}}
	out := m.detailContent(occ, st)
	// 見出し直下のサブタイトルとして topic（deprecated_versions の値）が出る。
	if !strings.Contains(out, "AWS Lambda end of support for Python 3.9") {
		t.Fatalf("detail content missing topic subtitle: %q", out)
	}
	// 旧来の "metadata: key=value" 行はもう出さない。
	if strings.Contains(out, "deprecated_versions=") {
		t.Fatalf("should not show raw metadata key=value: %q", out)
	}
}

func TestWindowResizeDoesNotPanic(t *testing.T) {
	m := newTestModel(sampleOccs(), "")
	for _, sz := range [][2]int{{120, 40}, {5, 2}, {0, 0}, {1, 1}} {
		m, _ = update(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		_ = m.View() // 描画してもパニックしないこと。
	}
}

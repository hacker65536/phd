package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hacker65536/phd/internal/model"
)

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

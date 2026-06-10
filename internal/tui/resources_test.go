package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/hacker65536/phd/internal/model"
)

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

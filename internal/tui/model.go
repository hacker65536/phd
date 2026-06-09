// Package tui は phd の対話的ドリルダウン UI（Bubble Tea）。
// 一覧 → Enter で1階層下 → 影響リソース/詳細を遅延ロード → Esc で戻る。
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"phd/internal/cache"
	"phd/internal/group"
	"phd/internal/health"
	"phd/internal/model"
)

// Input は main から渡す TUI の入力一式。enrich 前の論理イベントと、
// 遅延取得（詳細/リソース/アカウント名）に必要なクライアント・キャッシュを持つ。
type Input struct {
	Client  *health.Client
	Org     bool
	Events  []model.LogicalEvent // フィルタ・マージ・ソート済み（enrich 前）
	Now     time.Time
	Cache   *cache.Cache
	NS      string
	Cfg     aws.Config
	GroupBy string // "" | "type" | "topic"。指定時は group をルート階層にする
}

// loadState は遅延ロードの状態機械。
type loadState int

const (
	stateNone    loadState = iota // 未取得
	stateLoading                  // 取得中
	stateLoaded                   // 取得済み
	stateFailed                   // 失敗
)

// occState は 1 occurrence 分の遅延ロード結果（メモリ内キャッシュ）。
type occState struct {
	detail    health.Detail
	resources []model.Resource
	dState    loadState
	rState    loadState
	err       error
}

// navLevel はドリルダウンの階層。
type navLevel int

const (
	levelGroups    navLevel = iota // 0: EventGroup 一覧（--group-by 時のみ）
	levelOccs                      // 1: LogicalEvent（occurrence）一覧
	levelDetail                    // 2: 1 occurrence の詳細（メタ＋説明）
	levelResources                 // 3: 影響リソース一覧（独立ページ）
)

// frame はナビゲーションスタックの 1 階層。戻ったときにカーソル位置を復元するため cursor を保持する。
type frame struct {
	level  navLevel
	title  string
	items  []list.Item // levelGroups / levelOccs 用
	cursor int         // 離脱時のリスト選択位置
	// フィルタは 2 系統に分離して保持する:
	facets []string           // 低カーディナリティ軸（svc:/cat:/st:/reg:）の確定チップ → status line
	free   string             // 自由語検索（type:/re:/bare）→ 入力欄に残す
	occ    model.LogicalEvent // levelDetail 用（選択された occurrence）
	occKey string             // levelDetail 用（occState のキー = 代表 ARN）
}

// Model は Bubble Tea のモデル。
type Model struct {
	ctx context.Context
	in  *Input
	org bool
	now time.Time

	accountNames map[string]string // org の ID→名前（遅延取得）

	stack  []frame
	width  int
	height int

	list    list.Model
	detail  viewport.Model
	spinner spinner.Model

	fi        textinput.Model // 自前のフィルタ入力欄（使い捨て。確定後は status line に表示）
	filtering bool            // フィルタ入力中か

	picker       *pickerSpec     // 表示中のチェックボックス・ピッカー（nil=非表示）
	pickerSel    map[string]bool // 選択状態
	pickerCursor int             // パネル内カーソル位置

	showResolved bool // 影響リソース一覧で RESOLVED も表示するか（既定 false=非表示）

	state    map[string]*occState // key=代表ARN
	quitting bool
}

// New はモデルを構築する（Run と test から共用）。
func New(ctx context.Context, in *Input) Model {
	delegate := list.NewDefaultDelegate()
	l := list.New(nil, delegate, 80, 20)
	l.SetShowStatusBar(false)    // "N items" は冗長（件数は Title に集約）
	l.SetFilteringEnabled(false) // 絞り込みは自前で行う（bubbles 組み込みは使わない）
	l.SetShowHelp(false)

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	ti := textinput.New()
	ti.Prompt = "search: "
	ti.Placeholder = "keyword…"

	m := Model{
		ctx:     ctx,
		in:      in,
		org:     in.Org,
		now:     in.Now,
		list:    l,
		detail:  viewport.New(80, 18),
		spinner: sp,
		fi:      ti,
		state:   make(map[string]*occState),
	}
	m.stack = []frame{m.rootFrame()}
	m.syncListFromTop()
	return m
}

// rootFrame は GroupBy に応じてルート階層（group 一覧 or occurrence 一覧）を作る。
func (m *Model) rootFrame() frame {
	if m.in.GroupBy == "type" || m.in.GroupBy == "topic" {
		var groups []model.EventGroup
		topic := m.in.GroupBy == "topic"
		if topic {
			groups = group.ByTopic(m.in.Events)
		} else {
			groups = group.ByEventType(m.in.Events)
		}
		items := make([]list.Item, 0, len(groups))
		for _, g := range groups {
			items = append(items, groupItem{g: g, topic: topic, now: m.now})
		}
		return frame{level: levelGroups, title: groupNoun(m.in.GroupBy), items: items}
	}
	items := make([]list.Item, 0, len(m.in.Events))
	for _, e := range m.in.Events {
		items = append(items, occItem{ev: e, now: m.now})
	}
	return frame{level: levelOccs, title: occNoun(), items: items}
}

// Init は初期コマンド（org のアカウント名取得・spinner）。
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if m.org {
		cmds = append(cmds, m.loadAccountsCmd())
	}
	return tea.Batch(cmds...)
}

// top は現在のフレーム（スタック最上位）。
func (m *Model) top() *frame {
	return &m.stack[len(m.stack)-1]
}

// searchLineShown は検索入力行（"search: ..."）を表示すべきか（入力中 or 自由語あり）。
func (m *Model) searchLineShown() bool {
	return m.filtering || strings.TrimSpace(m.top().free) != ""
}

// chipLineShown はチップ行（"filters: ..."）を表示すべきか。
// category/status は下部 status line に出すのでチップ対象は svc/reg 等の非 cat/st 軸のみ。
func (m *Model) chipLineShown() bool {
	for _, f := range m.top().facets {
		if fld, _ := enumField(f); fld != "cat" && fld != "st" {
			return true
		}
	}
	return false
}

// catStatusShown は下部 status line（category/status の現在値）を出すべきか。
func (m *Model) catStatusShown() bool {
	for _, f := range m.top().facets {
		if fld, _ := enumField(f); fld == "cat" || fld == "st" {
			return true
		}
	}
	return false
}

// topLines / bottomLines は上部ヘッダ・下部の行数。relayout の高さ計算に使う。
func (m *Model) topLines() int {
	n := 0
	if m.searchLineShown() {
		n++
	}
	if m.chipLineShown() {
		n++
	}
	return n
}

func (m *Model) bottomLines() int {
	n := 1 // フッタ
	if m.catStatusShown() {
		n++
	}
	return n
}

// syncListFromTop は最上位フレームが一覧階層なら list の items/フィルタ/選択位置を同期する。
func (m *Model) syncListFromTop() {
	t := m.top()
	if t.level == levelDetail {
		return
	}
	m.fi.SetValue(t.free) // 自由語は入力欄へ復元
	m.relayout()          // ヘッダ有無に合わせて list 高さを調整
	m.applyEffective()
	if n := len(m.list.VisibleItems()); t.cursor >= 0 && t.cursor < n {
		m.list.Select(t.cursor)
	} else {
		m.list.Select(0)
	}
}

// updateTitle は Title に件数を集約して表示する（フィルタで減っていれば "M/N"、無ければ "N"）。
// 件数はここ 1 箇所だけに出す（bubbles の status bar とチップ行の件数は廃止）。
func (m *Model) updateTitle() {
	t := m.top()
	total := len(t.items)
	shown := len(m.list.Items())
	cnt := fmt.Sprintf("%d", total)
	if shown != total {
		cnt = fmt.Sprintf("%d/%d", shown, total)
	}
	m.list.Title = fmt.Sprintf("Health events — %s %s", cnt, t.title)
}

// effectiveQuery は最上位フレームの確定フィルタ（チップ + 自由語）を 1 本のクエリ文字列に合成する。
func (m *Model) effectiveQuery() string {
	t := m.top()
	return strings.TrimSpace(strings.Join(t.facets, " ") + " " + t.free)
}

// applyEffective は確定フィルタを list に反映する。
func (m *Model) applyEffective() {
	m.list.SetItems(filterItems(m.effectiveQuery(), m.top().items))
	m.updateTitle()
}

// previewItems はチップ + 与えた自由語（未確定の入力）でライブ絞り込みを反映する。
func (m *Model) previewItems(live string) {
	q := strings.TrimSpace(strings.Join(m.top().facets, " ") + " " + live)
	m.list.SetItems(filterItems(q, m.top().items))
	m.updateTitle()
}

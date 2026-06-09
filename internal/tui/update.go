package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"phd/internal/model"
)

// Update は Bubble Tea のメッセージループ。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case detailLoadedMsg:
		if st := m.state[msg.key]; st != nil {
			st.detail = msg.detail
			st.dState = stateLoaded
		}
		m.refreshDetailIfViewing(msg.key)
		return m, nil

	case resLoadedMsg:
		if st := m.state[msg.key]; st != nil {
			applyNames(msg.resources, m.accountNames)
			st.resources = msg.resources
			st.rState = stateLoaded
		}
		m.refreshDetailIfViewing(msg.key)
		return m, nil

	case accountsLoadedMsg:
		m.accountNames = msg.names
		// 既にロード済みのリソースにも遡及適用。
		for _, st := range m.state {
			if st.rState == stateLoaded {
				applyNames(st.resources, m.accountNames)
			}
		}
		if t := m.top(); t.level == levelDetail {
			m.refreshDetailIfViewing(t.occKey)
		}
		return m, nil

	case loadErrMsg:
		switch msg.kind {
		case "detail":
			if st := m.state[msg.key]; st != nil {
				st.dState = stateFailed
				st.err = msg.err
			}
		case "resources":
			if st := m.state[msg.key]; st != nil {
				st.rState = stateFailed
				st.err = msg.err
			}
		}
		m.refreshDetailIfViewing(msg.key)
		return m, nil

	default:
		// それ以外（spinner.TickMsg / list の FilterMatchesMsg・カーソル点滅など）。
		// 特に list の絞り込みは FilterMatchesMsg の到着で適用されるため、必ず list へ転送する。
		var sc, lc tea.Cmd
		m.spinner, sc = m.spinner.Update(msg)
		m.list, lc = m.list.Update(msg)
		return m, tea.Batch(sc, lc)
	}
}

// handleKey はキー入力を階層・モードに応じて処理する。
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		m.quitting = true
		return m, tea.Quit
	}

	// チェックボックス・ピッカー（category / status）表示中。
	if m.picker != nil {
		n := len(m.picker.values)
		switch msg.String() {
		case "up", "k":
			m.pickerCursor = (m.pickerCursor - 1 + n) % n
		case "down", "j":
			m.pickerCursor = (m.pickerCursor + 1) % n
		case " ", "x":
			v := m.picker.values[m.pickerCursor]
			m.pickerSel[v] = !m.pickerSel[v]
		case "enter":
			return m.applyPicker()
		case "esc", "q":
			m.picker = nil
		}
		return m, nil
	}

	// フィルタ入力中（自前の textinput）。Enter=確定 / Esc=取消、それ以外は入力欄へ。
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			// enum 軸（svc:/cat:/st:/reg:）はチップへ昇格、それ以外は自由語として入力欄に残す。
			chips, free := splitQuery(m.fi.Value())
			t := m.top()
			for _, c := range chips {
				t.facets = upsertFacet(t.facets, c)
			}
			t.free = strings.TrimSpace(free)
			m.fi.SetValue(t.free)
			m.filtering = false
			m.fi.Blur()
			m.relayout()
			m.applyEffective()
			m.list.Select(0)
			return m, nil
		case tea.KeyEsc:
			m.filtering = false
			m.fi.Blur()
			m.fi.SetValue(m.top().free) // 取消＝確定済みの自由語へ戻す
			m.relayout()
			m.applyEffective()
			return m, nil
		default:
			var cmd tea.Cmd
			m.fi, cmd = m.fi.Update(msg)
			m.previewItems(m.fi.Value()) // チップ + 入力中の自由語でライブ絞り込み
			return m, cmd
		}
	}

	if m.top().level == levelDetail {
		switch msg.String() {
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "enter", "l", "right":
			return m.drillToResources() // 影響リソース一覧（3ページ目）へ
		case "esc", "backspace", "left", "h":
			return m.goBack()
		default:
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg) // 説明のスクロール
			return m, cmd
		}
	}

	if m.top().level == levelResources {
		switch msg.String() {
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "a":
			// RESOLVED の表示/非表示トグル（スクロール位置は維持）。
			m.showResolved = !m.showResolved
			m.detail.SetContent(m.resourcesContent(m.state[m.top().occKey]))
			return m, nil
		case "e":
			// 現在表示中（visible）のリソースを CSV にエクスポート。
			return m.exportResources()
		case "esc", "backspace", "left", "h":
			return m.goBack()
		default:
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg) // リソース一覧のスクロール
			return m, cmd
		}
	}

	switch msg.String() {
	case "q":
		m.quitting = true
		return m, tea.Quit
	case "c":
		return m.openPicker(&categoryPicker)
	case "s":
		return m.openPicker(&statusPicker)
	case "/":
		// フィルタ入力を開始。入力欄には確定済みの自由語が入っている（enum チップは status line）。
		m.filtering = true
		m.relayout()
		m.previewItems(m.fi.Value())
		return m, m.fi.Focus()
	case "enter", "l", "right":
		return m.drillDown()
	case "esc":
		// 確定フィルタ（チップ or 自由語）があれば一括解除、なければ 1 階層戻る（2 段階）。
		t := m.top()
		if len(t.facets) > 0 || strings.TrimSpace(t.free) != "" {
			t.facets = nil
			t.free = ""
			m.fi.SetValue("")
			m.relayout()
			m.applyEffective()
			m.list.Select(0)
			return m, nil
		}
		return m.goBack()
	case "backspace":
		return m.goBack()
	default:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
}

// drillDown は現階層の選択行を確定して 1 階層下へ進む。
// levelOccs→levelDetail のときは詳細・リソースの遅延ロードを発火する。
func (m Model) drillDown() (tea.Model, tea.Cmd) {
	t := m.top()
	switch t.level {
	case levelGroups:
		gi, ok := m.list.SelectedItem().(groupItem)
		if !ok {
			return m, nil
		}
		t.cursor = m.list.Index()
		occs := gi.g.Occurrences
		items := make([]list.Item, 0, len(occs))
		for _, e := range occs {
			items = append(items, occItem{ev: e, now: m.now})
		}
		m.stack = append(m.stack, frame{
			level: levelOccs,
			title: occNoun(),
			items: items,
		})
		m.syncListFromTop()
		return m, nil

	case levelOccs:
		oi, ok := m.list.SelectedItem().(occItem)
		if !ok {
			return m, nil
		}
		t.cursor = m.list.Index()
		key := firstARN(oi.ev)
		m.stack = append(m.stack, frame{
			level:  levelDetail,
			title:  oi.ev.EventTypeCode,
			occ:    oi.ev,
			occKey: key,
		})
		cmd := m.enterDetail(key, oi.ev)
		return m, cmd

	default:
		return m, nil
	}
}

// enterDetail は詳細画面へ入る。未ロードなら遅延ロードを発火し、ロード済みなら即座に内容を表示する。
func (m *Model) enterDetail(key string, ev model.LogicalEvent) tea.Cmd {
	st := m.state[key]
	if st == nil {
		st = &occState{}
		m.state[key] = st
	}
	var cmds []tea.Cmd
	if key == "" {
		// ARN 不明（生イベントが空）。取得不能としてマークだけする。
		st.dState = stateFailed
		st.rState = stateFailed
	} else {
		if st.dState == stateNone {
			st.dState = stateLoading
			cmds = append(cmds, m.loadDetailCmd(key, ev))
		}
		if st.rState == stateNone {
			st.rState = stateLoading
			cmds = append(cmds, m.loadResourcesCmd(key, ev))
		}
	}
	m.detail.GotoTop()
	m.detail.SetContent(m.detailContent(ev, st))
	if len(cmds) > 0 {
		cmds = append(cmds, m.spinner.Tick)
	}
	return tea.Batch(cmds...)
}

// drillToResources は詳細（level 2）から影響リソース一覧（level 3）へ進む。
// リソースは詳細入場時に既にロード中/済みなので、ここでは表示するだけ。
func (m Model) drillToResources() (tea.Model, tea.Cmd) {
	t := m.top()
	m.flash = "" // ページ移動でエクスポート結果メッセージをクリア
	m.stack = append(m.stack, frame{
		level:  levelResources,
		title:  t.title,
		occ:    t.occ,
		occKey: t.occKey,
	})
	m.showCurrentPage()
	return m, nil
}

// showCurrentPage は現在のページ（詳細 or リソース一覧）の内容を viewport に設定し先頭へ。
func (m *Model) showCurrentPage() {
	t := m.top()
	switch t.level {
	case levelDetail:
		m.detail.SetContent(m.detailContent(t.occ, m.state[t.occKey]))
		m.detail.GotoTop()
	case levelResources:
		m.detail.SetContent(m.resourcesContent(m.state[t.occKey]))
		m.detail.GotoTop()
	}
}

// openPicker はチェックボックス・ピッカーを開き、現在のチップから選択状態を復元する。
func (m Model) openPicker(spec *pickerSpec) (tea.Model, tea.Cmd) {
	m.picker = spec
	m.pickerCursor = 0
	m.pickerSel = currentSelection(spec.field, spec.values, m.top().facets)
	return m, nil
}

// applyPicker はピッカーの選択を field: チップ（カンマ区切り OR）に反映して閉じる。
func (m Model) applyPicker() (tea.Model, tea.Cmd) {
	field := m.picker.field
	var selected []string
	for _, v := range m.picker.values {
		if m.pickerSel[v] {
			selected = append(selected, v)
		}
	}
	t := m.top()
	t.facets = removeFacet(t.facets, field)
	if len(selected) > 0 {
		t.facets = upsertFacet(t.facets, field+":"+strings.Join(selected, ","))
	}
	m.picker = nil
	m.relayout()
	m.applyEffective()
	m.list.Select(0)
	return m, nil
}

// goBack は 1 階層戻る（最上位なら何もしない）。
func (m Model) goBack() (tea.Model, tea.Cmd) {
	if len(m.stack) <= 1 {
		return m, nil
	}
	m.flash = "" // ページ移動でエクスポート結果メッセージをクリア
	m.stack = m.stack[:len(m.stack)-1]
	m.syncListFromTop() // 一覧階層なら list を同期（detail/resources では no-op）
	m.showCurrentPage() // detail/resources に戻ったら該当ページを再描画
	return m, nil
}

// refreshDetailIfViewing は現在その occurrence の詳細/リソースを表示中なら viewport を再描画する
// （遅延ロード到着時。スクロール位置は維持するため GotoTop しない）。
func (m *Model) refreshDetailIfViewing(key string) {
	t := m.top()
	if t.occKey != key {
		return
	}
	switch t.level {
	case levelDetail:
		m.detail.SetContent(m.detailContent(t.occ, m.state[key]))
	case levelResources:
		m.detail.SetContent(m.resourcesContent(m.state[key]))
	}
}

// relayout は端末サイズに合わせて list / viewport を再構成する。
func (m *Model) relayout() {
	w, h := m.width, m.height
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	// 上部ヘッダ（検索行/チップ行）と下部（status line/フッタ）を除いた領域を子コンポーネントへ。
	body := h - m.topLines() - m.bottomLines()
	if body < 3 {
		body = 3
	}
	m.list.SetSize(w, body)
	m.detail.Width = w
	m.detail.Height = h - 2 // 詳細画面はヘッダ(1)+フッタ(1)のみ
	if m.detail.Height < 3 {
		m.detail.Height = 3
	}
	if iw := w - 10; iw > 0 {
		m.fi.Width = iw
	}
}

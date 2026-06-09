package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"phd/internal/cache"
	"phd/internal/health"
	"phd/internal/model"
	"phd/internal/orgs"
)

// 遅延ロードの到着メッセージ。AWS 呼び出しは全て tea.Cmd（下記）の中に隔離し、
// Update はこれらのメッセージを受けて状態を更新するだけなので、テストはメッセージ注入で行える。
type detailLoadedMsg struct {
	key    string
	detail health.Detail
}

type resLoadedMsg struct {
	key       string
	resources []model.Resource
}

type accountsLoadedMsg struct {
	names map[string]string
}

type loadErrMsg struct {
	key  string
	kind string // "detail" | "resources" | "accounts"
	err  error
}

// loadDetailCmd は 1 occurrence の説明・メタデータを取得する。
func (m Model) loadDetailCmd(key string, occ model.LogicalEvent) tea.Cmd {
	ctx, c, org := m.ctx, m.in.Client, m.org
	return func() tea.Msg {
		d, err := c.FetchDetails(ctx, org, key)
		if err != nil {
			return loadErrMsg{key: key, kind: "detail", err: err}
		}
		return detailLoadedMsg{key: key, detail: d}
	}
}

// loadResourcesCmd は 1 occurrence の影響リソースを全 region 分取得して平坦化する。
func (m Model) loadResourcesCmd(key string, occ model.LogicalEvent) tea.Cmd {
	ctx, c, org := m.ctx, m.in.Client, m.org
	return func() tea.Msg {
		var all []model.Resource
		for _, raw := range occ.RawEvents {
			if raw.Arn == "" {
				continue
			}
			res, err := c.FetchResources(ctx, org, raw.Arn, raw.Region)
			if err != nil {
				return loadErrMsg{key: key, kind: "resources", err: err}
			}
			all = append(all, res...)
		}
		return resLoadedMsg{key: key, resources: all}
	}
}

// loadAccountsCmd は org の ID→名前 マップを取得する（24h キャッシュ）。
func (m Model) loadAccountsCmd() tea.Cmd {
	ctx, ca, ns, cfg := m.ctx, m.in.Cache, m.in.NS, m.in.Cfg
	return func() tea.Msg {
		names, err := cache.Fetch(ca, ns+"|accounts", cache.AccountsTTL, func() (map[string]string, error) {
			return orgs.New(cfg).NameMap(ctx)
		})
		if err != nil {
			return loadErrMsg{kind: "accounts", err: err}
		}
		return accountsLoadedMsg{names: names}
	}
}

// applyNames はリソース配列にアカウント名を付与する（map が無ければ何もしない）。
func applyNames(res []model.Resource, names map[string]string) {
	if len(names) == 0 {
		return
	}
	for i := range res {
		if n, ok := names[res[i].AccountID]; ok {
			res[i].AccountName = n
		}
	}
}

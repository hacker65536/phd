// Package enrich は論理イベントに影響リソースとアカウント名を付与する。
package enrich

import (
	"context"
	"sort"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/hacker65536/phd/internal/health"
	"github.com/hacker65536/phd/internal/model"
)

// maxConcurrency は影響リソース取得の同時実行数の上限（Health の 429 を避けるため控えめに）。
const maxConcurrency = 4

// Resources は各論理イベントの元イベント（ARN×region）ごとに影響リソースを並列取得し、
// LogicalEvent.Resources へ平坦化、LogicalEvent.Accounts にアカウント ID 集合を設定する。
func Resources(ctx context.Context, c *health.Client, org bool, events []model.LogicalEvent) error {
	type job struct {
		idx    int
		arn    string
		region string
	}
	var jobs []job
	for i := range events {
		for _, raw := range events[i].RawEvents {
			if raw.Arn == "" {
				continue
			}
			jobs = append(jobs, job{idx: i, arn: raw.Arn, region: raw.Region})
		}
	}

	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxConcurrency)
	for _, j := range jobs {
		j := j
		g.Go(func() error {
			res, err := c.FetchResources(gctx, org, j.arn, j.region)
			if err != nil {
				return err
			}
			mu.Lock()
			events[j.idx].Resources = append(events[j.idx].Resources, res...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	for i := range events {
		events[i].Accounts = uniqueAccounts(events[i].Resources)
	}
	return nil
}

// Details は各論理イベントの説明文・eventMetadata を取得し、Topic ラベルを計算する。
// 代表として最初の元イベント ARN を用いる（同一 occurrence の詳細は共通）。
// 取得は FetchDetailsBatch に委譲し、未キャッシュ分を 10 件ずつまとめて叩く（per-event 大量呼び出しを回避）。
func Details(ctx context.Context, c *health.Client, org bool, events []model.LogicalEvent) error {
	reqs := make([]health.DetailReq, 0, len(events))
	for i := range events {
		if arn, scope := firstRaw(events[i]); arn != "" {
			reqs = append(reqs, health.DetailReq{Arn: arn, ScopeCode: scope})
		}
	}
	// FetchDetailsBatch はエラー時も取得済みの分を res に残す。部分結果を先に反映してから err を返す
	// （呼び出し側は warning を出しつつ「取得できた段階のデータ」で描画/グルーピングを続けられる）。
	res, err := c.FetchDetailsBatch(ctx, org, reqs)
	for i := range events {
		arn, _ := firstRaw(events[i])
		if d, ok := res[arn]; ok {
			events[i].Description = d.Description
			events[i].Metadata = d.Metadata
			events[i].Topic = TopicLabel(d.Metadata)
		}
	}
	return err
}

// firstRaw は論理イベントの代表 ARN と eventScopeCode（最初の非空 ARN の元イベント）を返す。
func firstRaw(e model.LogicalEvent) (arn, scope string) {
	for _, raw := range e.RawEvents {
		if raw.Arn != "" {
			return raw.Arn, raw.ScopeCode
		}
	}
	return "", ""
}

// TopicLabel は eventMetadata の値を人間可読な話題ラベルに整形する（空なら ""）。
func TopicLabel(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vals := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := meta[k]; v != "" {
			vals = append(vals, v)
		}
	}
	return strings.Join(vals, "; ")
}

// ApplyAccountNames は ID→名前 マップを使ってリソースとアカウントに名前を付与する。
func ApplyAccountNames(events []model.LogicalEvent, names map[string]string) {
	for i := range events {
		for j := range events[i].Resources {
			if n, ok := names[events[i].Resources[j].AccountID]; ok {
				events[i].Resources[j].AccountName = n
			}
		}
		for j := range events[i].Accounts {
			if n, ok := names[events[i].Accounts[j].ID]; ok {
				events[i].Accounts[j].Name = n
			}
		}
	}
}

func uniqueAccounts(res []model.Resource) []model.Account {
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
	out := make([]model.Account, 0, len(ids))
	for _, id := range ids {
		out = append(out, model.Account{ID: id})
	}
	return out
}

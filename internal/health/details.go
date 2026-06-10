package health

import (
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshealth "github.com/aws/aws-sdk-go-v2/service/health"
	htypes "github.com/aws/aws-sdk-go-v2/service/health/types"
	"golang.org/x/sync/errgroup"

	"github.com/hacker65536/phd/internal/cache"
)

// detailBatchSize は DescribeEventDetails[ForOrganization] が 1 回で受け付けるイベント数の上限。
const detailBatchSize = 10

// detailBatchConcurrency はバッチ呼び出し／account 解決の並列上限。
const detailBatchConcurrency = 4

// Detail はイベントの詳細（変更内容説明と構造化メタデータ）。
type Detail struct {
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"` // 例: {"deprecated_versions": "... Python 3.9"}
}

// DetailReq は詳細取得の 1 リクエスト。ScopeCode が ACCOUNT_SPECIFIC の org イベントは
// account id が必要なため、フィルタ構築時に影響アカウントを 1 つ解決して付与する。
type DetailReq struct {
	Arn       string
	ScopeCode string // model.Event.ScopeCode（PUBLIC|ACCOUNT_SPECIFIC|NONE）
}

func (c *Client) detailKey(org bool, eventArn string) string {
	return fmt.Sprintf("%s|detail|org=%v|%s", c.ns, org, eventArn)
}

// FetchDetails は単一イベント（ARN）の説明文と eventMetadata を取得する（TUI の遅延ロード用）。
func (c *Client) FetchDetails(ctx context.Context, org bool, eventArn string) (Detail, error) {
	return cache.Fetch(c.cache, c.detailKey(org, eventArn), c.ttl, func() (Detail, error) {
		if org {
			return c.fetchDetailOrg(ctx, eventArn)
		}
		return c.fetchDetailAccount(ctx, eventArn)
	})
}

// FetchDetailsBatch は複数イベントの詳細をまとめて取得する（最大 detailBatchSize 件/API 呼び出し）。
// 未キャッシュの分だけを 10 件ずつ束ねて DescribeEventDetails[ForOrganization] を叩き、ARN で配り戻す。
// これにより per-event の大量並列呼び出しを避け、ThrottlingException(429) を抑える。
func (c *Client) FetchDetailsBatch(ctx context.Context, org bool, reqs []DetailReq) (map[string]Detail, error) {
	out := make(map[string]Detail)
	seen := make(map[string]bool)
	var misses []DetailReq
	for _, r := range reqs {
		if r.Arn == "" || seen[r.Arn] {
			continue
		}
		seen[r.Arn] = true
		if d, ok := cache.Peek[Detail](c.cache, c.detailKey(org, r.Arn), c.ttl); ok {
			out[r.Arn] = d
		} else {
			misses = append(misses, r)
		}
	}
	if len(misses) == 0 {
		return out, nil
	}
	var err error
	if org {
		err = c.fetchDetailsOrgBatch(ctx, misses, out)
	} else {
		err = c.fetchDetailsAccountBatch(ctx, misses, out)
	}
	return out, err
}

// fetchDetailsAccountBatch は account スコープのバッチ取得（DescribeEventDetails, 最大10 ARN/回）。
func (c *Client) fetchDetailsAccountBatch(ctx context.Context, misses []DetailReq, out map[string]Detail) error {
	arns := make([]string, 0, len(misses))
	for _, r := range misses {
		arns = append(arns, r.Arn)
	}
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(detailBatchConcurrency)
	for _, ch := range chunk(arns, detailBatchSize) {
		ch := ch
		g.Go(func() error {
			c.wait(gctx)
			c.cache.MarkMiss()
			res, err := c.api.DescribeEventDetails(gctx, &awshealth.DescribeEventDetailsInput{EventArns: ch})
			if err != nil {
				return err
			}
			mu.Lock()
			c.storeDetails(false, res.SuccessfulSet, out)
			mu.Unlock()
			return nil
		})
	}
	return g.Wait()
}

// fetchDetailsOrgBatch は org スコープのバッチ取得。account-specific は account id を解決して付与し、
// DescribeEventDetailsForOrganization に最大10フィルタ/回でまとめて投げる。
func (c *Client) fetchDetailsOrgBatch(ctx context.Context, misses []DetailReq, out map[string]Detail) error {
	// account-specific のみ account id を解決（PUBLIC/NONE は不要 → affected-accounts を叩かない）。
	acctOf := make(map[string]string)
	var mu sync.Mutex
	{
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(detailBatchConcurrency)
		for _, r := range misses {
			if r.ScopeCode != string(htypes.EventScopeCodeAccountSpecific) {
				continue
			}
			arn := r.Arn
			g.Go(func() error {
				ids, err := c.fetchAffectedAccountsOrg(gctx, arn)
				if err != nil {
					return err
				}
				if len(ids) > 0 {
					mu.Lock()
					acctOf[arn] = ids[0]
					mu.Unlock()
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
	}

	filters := make([]htypes.EventAccountFilter, 0, len(misses))
	for _, r := range misses {
		f := htypes.EventAccountFilter{EventArn: aws.String(r.Arn)}
		if a, ok := acctOf[r.Arn]; ok {
			f.AwsAccountId = aws.String(a)
		}
		filters = append(filters, f)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(detailBatchConcurrency)
	for _, ch := range chunk(filters, detailBatchSize) {
		ch := ch
		g.Go(func() error {
			c.wait(gctx)
			c.cache.MarkMiss()
			res, err := c.api.DescribeEventDetailsForOrganization(gctx,
				&awshealth.DescribeEventDetailsForOrganizationInput{OrganizationEventDetailFilters: ch})
			if err != nil {
				return err
			}
			mu.Lock()
			c.storeOrgDetails(res.SuccessfulSet, out)
			mu.Unlock()
			return nil
		})
	}
	return g.Wait()
}

// storeDetails は account 版レスポンスを ARN で out に詰め、各 ARN を個別キャッシュする。
func (c *Client) storeDetails(org bool, set []htypes.EventDetails, out map[string]Detail) {
	for _, d := range set {
		if d.Event == nil {
			continue
		}
		arn := aws.ToString(d.Event.Arn)
		det := detailFrom(d.EventDescription, d.EventMetadata)
		out[arn] = det
		cache.Put(c.cache, c.detailKey(org, arn), det)
	}
}

// storeOrgDetails は org 版レスポンスを ARN で out に詰め、各 ARN を個別キャッシュする。
func (c *Client) storeOrgDetails(set []htypes.OrganizationEventDetails, out map[string]Detail) {
	for _, d := range set {
		if d.Event == nil {
			continue
		}
		arn := aws.ToString(d.Event.Arn)
		det := detailFrom(d.EventDescription, d.EventMetadata)
		out[arn] = det
		cache.Put(c.cache, c.detailKey(true, arn), det)
	}
}

func (c *Client) fetchDetailAccount(ctx context.Context, eventArn string) (Detail, error) {
	c.wait(ctx)
	out, err := c.api.DescribeEventDetails(ctx, &awshealth.DescribeEventDetailsInput{
		EventArns: []string{eventArn},
	})
	if err != nil {
		return Detail{}, err
	}
	for _, d := range out.SuccessfulSet {
		return detailFrom(d.EventDescription, d.EventMetadata), nil
	}
	return Detail{}, nil
}

func (c *Client) fetchDetailOrg(ctx context.Context, eventArn string) (Detail, error) {
	// ACCOUNT_SPECIFIC なイベントは awsAccountId が無いと詳細が空になるため、
	// 影響アカウントを1つ取得して付与する（説明・メタデータはアカウントによらず共通）。
	filter := htypes.EventAccountFilter{EventArn: aws.String(eventArn)}
	if ids, err := c.fetchAffectedAccountsOrg(ctx, eventArn); err == nil && len(ids) > 0 {
		filter.AwsAccountId = aws.String(ids[0])
	}
	c.wait(ctx)
	out, err := c.api.DescribeEventDetailsForOrganization(ctx, &awshealth.DescribeEventDetailsForOrganizationInput{
		OrganizationEventDetailFilters: []htypes.EventAccountFilter{filter},
	})
	if err != nil {
		return Detail{}, err
	}
	for _, d := range out.SuccessfulSet {
		return detailFrom(d.EventDescription, d.EventMetadata), nil
	}
	return Detail{}, nil
}

func detailFrom(desc *htypes.EventDescription, meta map[string]string) Detail {
	d := Detail{Metadata: meta}
	if desc != nil {
		d.Description = aws.ToString(desc.LatestDescription)
	}
	return d
}

// chunk は s を最大 n 要素ずつに分割する。
func chunk[T any](s []T, n int) [][]T {
	if n <= 0 {
		n = 1
	}
	var out [][]T
	for i := 0; i < len(s); i += n {
		end := i + n
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

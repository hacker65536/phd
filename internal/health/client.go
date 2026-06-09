// Package health は AWS Health API の取得層。account/org 両スコープを同一の model.Event に正規化する。
package health

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshealth "github.com/aws/aws-sdk-go-v2/service/health"
	htypes "github.com/aws/aws-sdk-go-v2/service/health/types"
	"golang.org/x/time/rate"

	"phd/internal/cache"
	"phd/internal/model"
)

// rateLimit は Health API への送信レート上限（req/s）。アダプティブ・リトライと合わせて
// クライアント側でバーストを抑え、429 を避ける。
const (
	rateLimit = 8
	rateBurst = 8
)

// Client は Health API クライアントのラッパー。応答は cache で TTL キャッシュする。
type Client struct {
	api     *awshealth.Client
	cache   *cache.Cache
	ns      string        // キャッシュキーの名前空間（profile|region など）
	ttl     time.Duration // events / resources / details の TTL
	limiter *rate.Limiter // 送信レート制御（429 対策）
}

// New は設定から Client を生成する。ca が nil ならキャッシュ無効。
func New(cfg aws.Config, ca *cache.Cache, ns string, ttl time.Duration) *Client {
	return &Client{
		api:     awshealth.NewFromConfig(cfg),
		cache:   ca,
		ns:      ns,
		ttl:     ttl,
		limiter: rate.NewLimiter(rate.Limit(rateLimit), rateBurst),
	}
}

// wait はレート制御。1 トークン取得できるまで待つ（limiter が nil なら何もしない）。
func (c *Client) wait(ctx context.Context) {
	if c.limiter != nil {
		_ = c.limiter.Wait(ctx)
	}
}

// fetchAffectedAccountsOrg は org イベントの影響アカウント ID 一覧を返す（キャッシュ共有）。
// 詳細取得（account-specific の account 解決）と影響リソース取得の両方から使い、重複呼び出しを避ける。
func (c *Client) fetchAffectedAccountsOrg(ctx context.Context, eventArn string) ([]string, error) {
	key := fmt.Sprintf("%s|affaccts|%s", c.ns, eventArn)
	return cache.Fetch(c.cache, key, c.ttl, func() ([]string, error) {
		var ids []string
		p := awshealth.NewDescribeAffectedAccountsForOrganizationPaginator(c.api,
			&awshealth.DescribeAffectedAccountsForOrganizationInput{EventArn: aws.String(eventArn)})
		for p.HasMorePages() {
			c.wait(ctx)
			page, err := p.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			ids = append(ids, page.AffectedAccounts...)
		}
		return ids, nil
	})
}

// Query は取得時の選択条件。空フィールド（ゼロ値）は未指定として無視する。
// 既定は Statuses による選択のみで、時間窓は持たない（upcoming を取りこぼさないため）。
type Query struct {
	Statuses        []string  // open|upcoming|closed（空なら API 側は全件）
	LastUpdatedFrom time.Time // --since の下限
	LastUpdatedTo   time.Time
	StartFrom       time.Time // --starting のレンジ
	StartTo         time.Time
}

func (q Query) key() string {
	return fmt.Sprintf("st=%v|lu=%d-%d|sd=%d-%d",
		q.Statuses, q.LastUpdatedFrom.Unix(), q.LastUpdatedTo.Unix(), q.StartFrom.Unix(), q.StartTo.Unix())
}

func statusCodes(ss []string) []htypes.EventStatusCode {
	out := make([]htypes.EventStatusCode, 0, len(ss))
	for _, s := range ss {
		out = append(out, htypes.EventStatusCode(s))
	}
	return out
}

// dtr は from/to の少なくとも一方が非ゼロのとき DateTimeRange を返す（無指定の端は省略）。
func dtr(from, to time.Time) (htypes.DateTimeRange, bool) {
	var r htypes.DateTimeRange
	if from.IsZero() && to.IsZero() {
		return r, false
	}
	if !from.IsZero() {
		r.From = aws.Time(from)
	}
	if !to.IsZero() {
		r.To = aws.Time(to)
	}
	return r, true
}

// FetchEvents は条件に合うイベントを取得して正規化する。
func (c *Client) FetchEvents(ctx context.Context, org bool, q Query) ([]model.Event, error) {
	key := fmt.Sprintf("%s|events|org=%v|%s", c.ns, org, q.key())
	return cache.Fetch(c.cache, key, c.ttl, func() ([]model.Event, error) {
		if org {
			return c.fetchOrg(ctx, q)
		}
		return c.fetchAccount(ctx, q)
	})
}

func (c *Client) fetchAccount(ctx context.Context, q Query) ([]model.Event, error) {
	filter := &htypes.EventFilter{}
	if len(q.Statuses) > 0 {
		filter.EventStatusCodes = statusCodes(q.Statuses)
	}
	if r, ok := dtr(q.LastUpdatedFrom, q.LastUpdatedTo); ok {
		filter.LastUpdatedTimes = []htypes.DateTimeRange{r}
	}
	if r, ok := dtr(q.StartFrom, q.StartTo); ok {
		filter.StartTimes = []htypes.DateTimeRange{r}
	}
	input := &awshealth.DescribeEventsInput{
		Filter:     filter,
		MaxResults: aws.Int32(100),
	}
	var out []model.Event
	p := awshealth.NewDescribeEventsPaginator(c.api, input)
	for p.HasMorePages() {
		c.wait(ctx)
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range page.Events {
			out = append(out, model.Event{
				Arn:           aws.ToString(e.Arn),
				Service:       aws.ToString(e.Service),
				EventTypeCode: aws.ToString(e.EventTypeCode),
				Category:      string(e.EventTypeCategory),
				Region:        aws.ToString(e.Region),
				ScopeCode:     string(e.EventScopeCode),
				StatusCode:    string(e.StatusCode),
				StartTime:     aws.ToTime(e.StartTime),
				EndTime:       aws.ToTime(e.EndTime),
				LastUpdated:   aws.ToTime(e.LastUpdatedTime),
			})
		}
	}
	return out, nil
}

func (c *Client) fetchOrg(ctx context.Context, q Query) ([]model.Event, error) {
	filter := &htypes.OrganizationEventFilter{}
	if len(q.Statuses) > 0 {
		filter.EventStatusCodes = statusCodes(q.Statuses)
	}
	if r, ok := dtr(q.LastUpdatedFrom, q.LastUpdatedTo); ok {
		filter.LastUpdatedTime = &r
	}
	if r, ok := dtr(q.StartFrom, q.StartTo); ok {
		filter.StartTime = &r
	}
	input := &awshealth.DescribeEventsForOrganizationInput{
		Filter:     filter,
		MaxResults: aws.Int32(100),
	}
	var out []model.Event
	p := awshealth.NewDescribeEventsForOrganizationPaginator(c.api, input)
	for p.HasMorePages() {
		c.wait(ctx)
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range page.Events {
			out = append(out, model.Event{
				Arn:           aws.ToString(e.Arn),
				Service:       aws.ToString(e.Service),
				EventTypeCode: aws.ToString(e.EventTypeCode),
				Category:      string(e.EventTypeCategory),
				Region:        aws.ToString(e.Region),
				ScopeCode:     string(e.EventScopeCode),
				StatusCode:    string(e.StatusCode),
				StartTime:     aws.ToTime(e.StartTime),
				EndTime:       aws.ToTime(e.EndTime),
				LastUpdated:   aws.ToTime(e.LastUpdatedTime),
			})
		}
	}
	return out, nil
}

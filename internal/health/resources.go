package health

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshealth "github.com/aws/aws-sdk-go-v2/service/health"
	htypes "github.com/aws/aws-sdk-go-v2/service/health/types"

	"phd/internal/cache"
	"phd/internal/model"
)

// FetchResources は単一イベント（ARN）の影響リソースを取得する。
// region はイベントが属するリージョンで、結果の各リソースに付与する。
// org=true では「影響アカウント→影響エンティティ」の 2 段で全アカウント分を平坦化する。
func (c *Client) FetchResources(ctx context.Context, org bool, eventArn, region string) ([]model.Resource, error) {
	key := fmt.Sprintf("%s|resources|org=%v|%s", c.ns, org, eventArn)
	return cache.Fetch(c.cache, key, c.ttl, func() ([]model.Resource, error) {
		if org {
			return c.fetchResourcesOrg(ctx, eventArn, region)
		}
		return c.fetchResourcesAccount(ctx, eventArn, region)
	})
}

func (c *Client) fetchResourcesAccount(ctx context.Context, eventArn, region string) ([]model.Resource, error) {
	input := &awshealth.DescribeAffectedEntitiesInput{
		Filter:     &htypes.EntityFilter{EventArns: []string{eventArn}},
		MaxResults: aws.Int32(100),
	}
	var out []model.Resource
	p := awshealth.NewDescribeAffectedEntitiesPaginator(c.api, input)
	for p.HasMorePages() {
		c.wait(ctx)
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, e := range page.Entities {
			out = append(out, model.Resource{
				AccountID: aws.ToString(e.AwsAccountId),
				Region:    region,
				Value:     aws.ToString(e.EntityValue),
				Status:    string(e.StatusCode),
			})
		}
	}
	return out, nil
}

func (c *Client) fetchResourcesOrg(ctx context.Context, eventArn, region string) ([]model.Resource, error) {
	// 1) 影響を受けるアカウントを取得（詳細取得と共有のキャッシュ付きヘルパを使い、重複呼び出しを避ける）
	accountIDs, err := c.fetchAffectedAccountsOrg(ctx, eventArn)
	if err != nil {
		return nil, err
	}
	if len(accountIDs) == 0 {
		return nil, nil
	}

	// 2) (eventArn, accountId) のフィルタで影響エンティティを取得。
	//    organizationEntityFilters は最大 10 件制約のため 10 アカウントずつバッチ。
	const batchSize = 10
	var out []model.Resource
	for start := 0; start < len(accountIDs); start += batchSize {
		end := start + batchSize
		if end > len(accountIDs) {
			end = len(accountIDs)
		}
		filters := make([]htypes.EventAccountFilter, 0, end-start)
		for _, id := range accountIDs[start:end] {
			filters = append(filters, htypes.EventAccountFilter{
				EventArn:     aws.String(eventArn),
				AwsAccountId: aws.String(id),
			})
		}
		input := &awshealth.DescribeAffectedEntitiesForOrganizationInput{
			OrganizationEntityFilters: filters,
			MaxResults:                aws.Int32(100),
		}
		p := awshealth.NewDescribeAffectedEntitiesForOrganizationPaginator(c.api, input)
		for p.HasMorePages() {
			c.wait(ctx)
			page, err := p.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, e := range page.Entities {
				out = append(out, model.Resource{
					AccountID: aws.ToString(e.AwsAccountId),
					Region:    region,
					Value:     aws.ToString(e.EntityValue),
					Status:    string(e.StatusCode),
				})
			}
		}
	}
	return out, nil
}

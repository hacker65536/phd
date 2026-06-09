// Package orgs は Organizations のアカウント ID→名前 解決を担う。
package orgs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
)

// Resolver はアカウント ID→名前 の解決機能。
type Resolver struct {
	api *organizations.Client
}

// New は設定から Resolver を生成する。
func New(cfg aws.Config) *Resolver {
	return &Resolver{api: organizations.NewFromConfig(cfg)}
}

// NameMap は全アカウントを列挙して ID→名前 のマップを返す。
// 管理アカウントでの実行と organizations:ListAccounts 権限が必要。
func (r *Resolver) NameMap(ctx context.Context) (map[string]string, error) {
	out := make(map[string]string)
	p := organizations.NewListAccountsPaginator(r.api, &organizations.ListAccountsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, a := range page.Accounts {
			out[aws.ToString(a.Id)] = aws.ToString(a.Name)
		}
	}
	return out, nil
}

// Package awsx は AWS SDK の設定ロードをまとめる。
package awsx

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// HealthRegion は AWS Health API のグローバルエンドポイントが属するリージョン。
// Health は us-east-1 でのみ提供されるため固定する。
const HealthRegion = "us-east-1"

// LoadConfig は profile（空なら既定）を解決し、リージョンを Health 用に固定した設定を返す。
// Health API は TPS 上限が低く ThrottlingException(429) を返しやすいため、アダプティブ・リトライを
// 有効化する（throttle を観測したら送信側が自動で絞り、full-jitter backoff で再試行する）。
func LoadConfig(ctx context.Context, profile string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithRegion(HealthRegion),
		config.WithRetryMode(aws.RetryModeAdaptive),
		config.WithRetryMaxAttempts(10),
	}
	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	return config.LoadDefaultConfig(ctx, opts...)
}

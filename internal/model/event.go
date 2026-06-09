// Package model defines AWS Health のデータを SDK 非依存で表現する型。
package model

import "time"

// Event は単一の Health イベント（region 単位）。account/org いずれの取得でも共通形に正規化する。
type Event struct {
	Arn           string    `json:"arn"`
	Service       string    `json:"service"`
	EventTypeCode string    `json:"eventTypeCode"`
	Category      string    `json:"category"` // issue|accountNotification|scheduledChange|investigation
	Region        string    `json:"region"`
	ScopeCode     string    `json:"scopeCode,omitempty"` // PUBLIC|ACCOUNT_SPECIFIC|NONE（詳細取得の最適化に使う）
	StatusCode    string    `json:"statusCode"`          // open|upcoming|closed
	StartTime     time.Time `json:"startTime"`
	EndTime       time.Time `json:"endTime"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

// Account は影響を受けるアカウント（ID と名前）。
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// Resource は影響を受けるリソース1件（平坦化表示の行）。
type Resource struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName,omitempty"`
	Region      string `json:"region"`
	Value       string `json:"value"`  // entityValue（リソース識別子）
	Status      string `json:"status"` // IMPAIRED|UNIMPAIRED|UNKNOWN|PENDING|RESOLVED など
}

// EventGroup は eventTypeCode 単位のロールアップ（最上位の抽象化層）。
// 別日程・別リージョンに散った occurrence を 1 つのイベントファミリーに束ねる。
type EventGroup struct {
	EventTypeCode   string         `json:"eventTypeCode"`
	Topic           string         `json:"topic,omitempty"` // topic グルーピング時の話題ラベル
	Service         string         `json:"service"`
	Category        string         `json:"category"`
	StatusCounts    map[string]int `json:"statusCounts"`    // open/upcoming/closed -> occurrence 数
	Regions         []string       `json:"regions"`         // 全 occurrence の region 和
	NextStatus      string         `json:"nextStatus"`      // NextStart の根拠ステータス
	NextStart       time.Time      `json:"nextStart"`       // 直近の予定開始（無ければゼロ）
	OccurrenceCount int            `json:"occurrenceCount"` // 配下 occurrence 数
	AccountCount    int            `json:"accountCount"`    // 影響アカウント数（取得時）
	ResourceCount   int            `json:"resourceCount"`   // 影響リソース総数（取得時）
	Description     string         `json:"description,omitempty"`
	Resources       []Resource     `json:"resources,omitempty"`   // 配下を平坦化（取得時）
	Occurrences     []LogicalEvent `json:"occurrences,omitempty"` // 配下の占有イベント
}

// LogicalEvent は region をまたいで同一 eventTypeCode を束ねた論理イベント。
type LogicalEvent struct {
	EventTypeCode string            `json:"eventTypeCode"`
	Service       string            `json:"service"`
	Category      string            `json:"category"`
	StatusCode    string            `json:"statusCode"` // 混在時は最も深刻なものを代表 (open>upcoming>closed)
	Regions       []string          `json:"regions"`
	StartTime     time.Time         `json:"startTime"`
	EndTime       time.Time         `json:"endTime"`
	Description   string            `json:"description,omitempty"` // 変更内容の説明（latestDescription）
	Metadata      map[string]string `json:"metadata,omitempty"`    // eventMetadata（topic グルーピング用）
	Topic         string            `json:"topic,omitempty"`       // metadata から導く話題ラベル
	Accounts      []Account         `json:"accounts,omitempty"`
	Resources     []Resource        `json:"resources,omitempty"`
	RawEvents     []Event           `json:"-"` // --no-merge / 詳細取得用に内部保持（JSON には出さない）
}

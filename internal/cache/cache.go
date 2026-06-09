// Package cache は AWS API 応答（正規化済み）の TTL 付きファイルキャッシュ。
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// TTL の既定値。
const (
	DefaultTTL  = time.Hour      // events / resources（更新頻度が低いので長め）
	AccountsTTL = 24 * time.Hour // アカウント名マップ
)

// Cache はキャッシュディレクトリと挙動を保持する。nil の Cache は常にミス（無効）として扱える。
type Cache struct {
	dir     string
	enabled bool
	refresh bool // true なら既存キャッシュを無視して必ず取得
	hits    int64
	misses  int64
}

// New はキャッシュを生成する。enabled=false で完全無効化。
func New(enabled, refresh bool) (*Cache, error) {
	c := &Cache{enabled: enabled, refresh: refresh}
	if !enabled {
		return c, nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	c.dir = filepath.Join(base, "phd")
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Cache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

// Fetch は key/ttl で結果をキャッシュする。ヒット時は復元し、ミス時は fn を呼んで保存する。
// c が nil または無効の場合は単に fn を実行する。
func Fetch[T any](c *Cache, key string, ttl time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	if c == nil || !c.enabled {
		return fn()
	}
	path := c.path(key)
	if !c.refresh {
		if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < ttl {
			data, err := os.ReadFile(path)
			if err == nil {
				var v T
				if json.Unmarshal(data, &v) == nil {
					atomic.AddInt64(&c.hits, 1)
					return v, nil
				}
			}
		}
	}
	atomic.AddInt64(&c.misses, 1)
	v, err := fn()
	if err != nil {
		return zero, err
	}
	if data, merr := json.Marshal(v); merr == nil {
		tmp := path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
	return v, nil
}

// Peek は key/ttl のキャッシュを取得するだけの読み取り（fn は呼ばない）。バッチ取得で
// 「未キャッシュの分だけまとめて API を叩く」ために使う。ヒット時は hits を加算する。
func Peek[T any](c *Cache, key string, ttl time.Duration) (T, bool) {
	var zero T
	if c == nil || !c.enabled || c.refresh {
		return zero, false
	}
	path := c.path(key)
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < ttl {
		if data, err := os.ReadFile(path); err == nil {
			var v T
			if json.Unmarshal(data, &v) == nil {
				atomic.AddInt64(&c.hits, 1)
				return v, true
			}
		}
	}
	return zero, false
}

// Put は key に値を保存する（Peek と対で使う）。
func Put[T any](c *Cache, key string, v T) {
	if c == nil || !c.enabled {
		return
	}
	if data, err := json.Marshal(v); err == nil {
		path := c.path(key)
		tmp := path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}

// MarkMiss は実 API 呼び出しを 1 回行ったことを記録する（バッチ API は 1 回で複数キーを埋めるため、
// Peek のミスごとではなく実際の呼び出し回数を数える用途）。
func (c *Cache) MarkMiss() {
	if c != nil {
		atomic.AddInt64(&c.misses, 1)
	}
}

// Hits / Misses はこれまでのキャッシュ命中数・ミス数（=実 API 呼び出し数）。
func (c *Cache) Hits() int64 {
	if c == nil {
		return 0
	}
	return atomic.LoadInt64(&c.hits)
}

func (c *Cache) Misses() int64 {
	if c == nil {
		return 0
	}
	return atomic.LoadInt64(&c.misses)
}

// Clear はキャッシュディレクトリ内の全エントリを削除する。
func (c *Cache) Clear() error {
	if c == nil || c.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_ = os.Remove(filepath.Join(c.dir, e.Name()))
	}
	return nil
}

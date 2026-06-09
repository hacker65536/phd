package tui

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

// Run は対話的 TUI を起動する。altscreen でフルスクリーン表示し、終了まで block する。
func Run(ctx context.Context, in *Input) error {
	if len(in.Events) == 0 {
		fmt.Fprintln(os.Stderr, "表示するイベントがありません。")
		return nil
	}
	p := tea.NewProgram(New(ctx, in), tea.WithAltScreen(), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

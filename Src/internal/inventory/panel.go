package inventory

import (
	"context"
	"html/template"
	"sync"
)

// ItemPanel 是其他模块挂到器材页上的一块内容.
type ItemPanel struct {
	Title string
	HTML  template.HTML
}

var (
	panelMu    sync.Mutex
	itemPanels []func(context.Context, int64) (ItemPanel, error)
)

// RegisterItemPanel 登记器材页上的附加内容. 库存模块不依赖调用方.
func RegisterItemPanel(fn func(context.Context, int64) (ItemPanel, error)) {
	if fn == nil {
		return
	}
	panelMu.Lock()
	defer panelMu.Unlock()
	itemPanels = append(itemPanels, fn)
}

// ItemPanels 返回已登记的附加内容.
func ItemPanels(ctx context.Context, id int64) ([]ItemPanel, error) {
	panelMu.Lock()
	current := append([]func(context.Context, int64) (ItemPanel, error)(nil), itemPanels...)
	panelMu.Unlock()
	var out []ItemPanel
	for _, fn := range current {
		panel, err := fn(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, panel)
	}
	return out, nil
}

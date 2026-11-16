package web

import (
	"context"
	"html/template"
	"sync"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/people"
)

// Widget 是首页上的一块汇总. 各模块在启动时注册, 首页按权限渲染.
type Widget struct {
	Title  string
	Allow  func(people.User) bool
	Render func(context.Context, people.User) (template.HTML, error)
}

type widgetView struct {
	Title string
	HTML  template.HTML
}

var (
	widgetMu sync.Mutex
	widgets  []Widget
)

// RegisterWidget 登记一个首页组件.
func RegisterWidget(widget Widget) {
	widgetMu.Lock()
	defer widgetMu.Unlock()
	widgets = append(widgets, widget)
}

func renderWidgets(ctx context.Context, user people.User) []widgetView {
	widgetMu.Lock()
	current := append([]Widget(nil), widgets...)
	widgetMu.Unlock()
	var out []widgetView
	for _, widget := range current {
		if widget.Allow != nil && !widget.Allow(user) {
			continue
		}
		if widget.Render == nil {
			continue
		}
		html, err := widget.Render(ctx, user)
		if err != nil {
			continue
		}
		out = append(out, widgetView{Title: widget.Title, HTML: html})
	}
	return out
}

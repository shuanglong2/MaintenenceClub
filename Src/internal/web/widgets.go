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

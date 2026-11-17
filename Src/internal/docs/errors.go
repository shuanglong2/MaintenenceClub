package docs

import "errors"

var (
	ErrNotFound  = errors.New("页面不存在")
	ErrBadInput  = errors.New("填写的内容不正确")
	ErrNotEmpty  = errors.New("请先移走里面的文档和子目录")
	ErrForbidden = errors.New("没有权限")
)

package logs

import "errors"

var (
	ErrNotFound  = errors.New("记录不存在")
	ErrBadInput  = errors.New("填写的内容不正确")
	ErrForbidden = errors.New("没有权限")
	ErrState     = errors.New("当前状态不能这样操作")
)

type textError struct {
	text string
	base error
}

func (e *textError) Error() string { return e.text }

func (e *textError) Unwrap() error { return e.base }

func errInput(text string) error { return &textError{text, ErrBadInput} }

package boards

import "errors"

var (
	ErrNotFound  = errors.New("没有找到")
	ErrForbidden = errors.New("没有权限")
	ErrBadInput  = errors.New("填写的内容不正确")
	ErrState     = errors.New("现在不能这样操作")
)

type textError struct {
	text string
	base error
}

func (e *textError) Error() string { return e.text }

func (e *textError) Unwrap() error { return e.base }

func errInput(text string) error { return &textError{text, ErrBadInput} }

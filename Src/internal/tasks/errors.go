package tasks

import "errors"

var (
	ErrNotFound  = errors.New("任务不存在")
	ErrBadInput  = errors.New("填写的内容不正确")
	ErrForbidden = errors.New("没有权限")
)

type textError struct {
	text string
	base error
}

func (e *textError) Error() string { return e.text }

func (e *textError) Unwrap() error { return e.base }

func errInput(text string) error { return &textError{text, ErrBadInput} }

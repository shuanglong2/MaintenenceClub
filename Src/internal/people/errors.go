package people

import "errors"

var (
	ErrLastLeader  = errors.New("至少保留一名在组组长")
	ErrBadLogin    = errors.New("登录名或密码不正确")
	ErrRateLimited = errors.New("尝试过于频繁, 请稍后再试")
	ErrForbidden   = errors.New("没有权限")
	ErrNotFound    = errors.New("没有这个人")
	ErrLoginTaken  = errors.New("登录名已存在")
	ErrNoSession   = errors.New("未登录")
	ErrExists      = errors.New("已经有人员, 请在网站上创建")
	ErrBadInput    = errors.New("填写的内容不正确")
)

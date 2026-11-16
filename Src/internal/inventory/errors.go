package inventory

import "errors"

var (
	ErrNotFound  = errors.New("物品不存在")
	ErrBadInput  = errors.New("填写的内容不正确")
	ErrForbidden = errors.New("没有权限")
	ErrStock     = errors.New("库存不足")
	ErrState     = errors.New("当前状态不能这样操作")
)

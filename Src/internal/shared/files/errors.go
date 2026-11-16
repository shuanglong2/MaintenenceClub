package files

import "errors"

var (
	ErrBadFile  = errors.New("不支持的文件类型")
	ErrTooBig   = errors.New("文件太大")
	ErrQuota    = errors.New("上传空间已满")
	ErrPixels   = errors.New("图片尺寸过大")
	ErrNotFound = errors.New("文件不存在")
)

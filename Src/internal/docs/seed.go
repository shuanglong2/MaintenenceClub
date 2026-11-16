package docs

import (
	"context"

	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/search"
)

const welcomeTitle = "欢迎使用文档"
const welcomeBody = `# 欢迎使用文档

这里存放维修组的资料. 页面使用 Markdown, 图片从编辑页上传后插入.

目录可以按主题整理. 仅管理层可见的页面, 组员在目录, 搜索和原文地址里都看不到.

两人同时修改同一页时, 后保存的人会看到冲突, 可以选择覆盖, 放弃, 或合并后再提交.
`

const backupTitle = "备份说明"
const backupBody = `# 备份说明

数据库每天 03:00 (北京时间) 自动备份, 保留最近 7 份, 文件在服务器的 backups/db 目录.

上传的图片和附件在 uploads 目录, 不在网站的公开路径里. 下载要先登录, 并且只能拿到自己有权查看的文件.
`

// EnsureWelcome 在还没有任何文档时放入两篇说明.
func (s *Service) EnsureWelcome(ctx context.Context) error {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM docs.pages`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var folderID int64
	if err := tx.QueryRow(ctx, `INSERT INTO docs.folders (name, sort_order) VALUES ('使用说明', 10) RETURNING id`).Scan(&folderID); err != nil {
		return err
	}
	for _, item := range []struct{ title, body string }{
		{welcomeTitle, welcomeBody},
		{backupTitle, backupBody},
	} {
		var id int64
		tokens := search.Join(item.title + "\n" + item.body)
		if err := tx.QueryRow(ctx, `
INSERT INTO docs.pages (folder_id, title, body, search_tokens, visibility)
VALUES ($1, $2, $3, $4, 'all') RETURNING id`, folderID, item.title, item.body, tokens).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
INSERT INTO docs.page_versions (page_id, version, title, body, editor_name)
VALUES ($1, 1, $2, $3, '系统')`, id, item.title, item.body); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

package docs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/shuanglong2/MaintenenceClub/Src/internal/shared/search"
)

const welcomeTitle = "欢迎使用文档"
const welcomeBody = `# 欢迎使用文档

这里存放维修组的资料. 页面使用 Markdown, 图片从编辑页上传后插入.

目录可以按主题整理. 仅管理层可见的页面, 组员在目录, 搜索和原文地址里都看不到.

两人同时修改同一页时, 后保存的人会看到冲突, 可以选择覆盖, 放弃, 或合并后再提交.
`

const backupTitle = "备份说明"
const previousBackupBody = `# 备份说明

数据库每天 03:00 (北京时间) 自动备份, 保留最近 7 份, 文件在服务器的 backups/db 目录.

上传的图片和附件在 uploads 目录, 不在网站的公开路径里. 下载要先登录, 并且只能拿到自己有权查看的文件.
`
const backupBody = `# 备份说明

数据库和上传目录每天 03:00 (北京时间) 自动备份, 各保留最近 7 份.

数据库文件在服务器的 backups/db. 上传目录用硬链接快照放在 backups/uploads. 备份总占用超过 8 GB 时会删掉较旧的快照.

上传的图片和附件不在网站的公开路径里. 下载要先登录, 并且只能拿到自己有权查看的文件.

组长可以在状态页看到最近一次备份是否成功. 每周应在另一台电脑上再拉一份副本.
`

const manualTitle = "各角色常用操作"
const manualBody = `# 各角色常用操作

## 组员

1. 登录后在首页看自己的任务和近期活动.
2. 在文档里查找仪器用法, 搜不到就问组长.
3. 活动页报名, 到场后让负责人签到.
4. 领用耗材前看库存数量, 不够就在任务里说明.
5. 干完活写一条工作日志, 报修写维修记录.

## 副组长

1. 活动开始前建活动, 结束后写总结.
2. 给任务指定负责人, 在看板上看进度.
3. 盘点时按位置核对数量, 再确认盘点单.
4. 维修记录里更换元件, 数量会自动扣库存.
5. 看审计, 确认谁改了文档或库存.

## 组长

1. 在人员页建立账号, 把初始密码交给本人, 需要时打开财务开关.
2. 财务页记收入, 支出, 存入和取出. 作废必须写原因, 原记录会留着.
3. 仅管理层可见的文档用来放内部约定.
4. 状态页看磁盘, 备份和版本. 红色提示要当天处理.
5. 备份每天自动做. 每周在另一台电脑上再拉一份副本.
6. 换届时先建好新组长, 再把自己改成其他角色或退出. 资料和账都留着.
`

const previousManualBody = `# 各角色常用操作

## 组员

1. 登录后在首页看自己的任务和近期活动.
2. 在文档里查找仪器用法, 搜不到就问组长.
3. 活动页报名, 到场后让负责人签到.
4. 领用耗材前看库存数量, 不够就在任务里说明.
5. 干完活写一条工作日志, 报修写维修记录.

## 副组长

1. 活动开始前建活动, 结束后写总结.
2. 给任务指定负责人, 在看板上看进度.
3. 盘点时按位置核对数量, 再确认盘点单.
4. 维修记录里更换元件, 数量会自动扣库存.
5. 看审计, 确认谁改了文档或库存.

## 组长

1. 在人员页建立账号, 把初始密码交给本人, 需要时打开财务开关.
2. 财务页记收入, 支出, 存入和取出. 作废必须写原因, 原记录会留着.
3. 仅管理层可见的文档用来放内部约定.
4. 状态页看磁盘, 备份和版本. 红色提示要当天处理.
5. 备份每天自动做. 每周在另一台电脑上再拉一份副本.
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

// EnsureManual 补上各角色常用操作, 并在备份说明仍是旧文案时改成含上传快照的版本.
// 已经改过的页面不会被覆盖.
func (s *Service) EnsureManual(ctx context.Context) error {
	if err := s.refreshBackup(ctx); err != nil {
		return err
	}
	if err := s.replaceSeed(ctx, manualTitle, manualBody, previousManualBody); err != nil {
		return err
	}
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM docs.pages WHERE title = $1 AND voided_at IS NULL`, manualTitle).Scan(&count); err != nil {
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
	err = tx.QueryRow(ctx, `SELECT id FROM docs.folders WHERE name = '使用说明' AND parent_id IS NULL ORDER BY id LIMIT 1`).Scan(&folderID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO docs.folders (name, sort_order) VALUES ('使用说明', 10) RETURNING id`).Scan(&folderID)
	}
	if err != nil {
		return err
	}
	tokens := search.Join(manualTitle + "\n" + manualBody)
	var id int64
	if err := tx.QueryRow(ctx, `
INSERT INTO docs.pages (folder_id, title, body, search_tokens, visibility)
VALUES ($1, $2, $3, $4, 'all') RETURNING id`, folderID, manualTitle, manualBody, tokens).Scan(&id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO docs.page_versions (page_id, version, title, body, editor_name)
VALUES ($1, 1, $2, $3, '系统')`, id, manualTitle, manualBody); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) refreshBackup(ctx context.Context) error {
	return s.replaceSeed(ctx, backupTitle, backupBody, previousBackupBody)
}

func (s *Service) replaceSeed(ctx context.Context, title, next, previous string) error {
	tokens := search.Join(title + "\n" + next)
	rows, err := s.pool.Query(ctx, `
UPDATE docs.pages
SET body = $1, search_tokens = $2, version = version + 1, updated_at = now()
WHERE title = $3 AND body = $4 AND voided_at IS NULL
RETURNING id, version`, next, tokens, title, previous)
	if err != nil {
		return err
	}
	defer rows.Close()
	type saved struct {
		id, version int64
	}
	var pages []saved
	for rows.Next() {
		var item saved
		if err := rows.Scan(&item.id, &item.version); err != nil {
			return err
		}
		pages = append(pages, item)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	for _, item := range pages {
		if _, err := s.pool.Exec(ctx, `
INSERT INTO docs.page_versions (page_id, version, title, body, editor_name)
VALUES ($1, $2, $3, $4, '系统')`, item.id, item.version, title, next); err != nil {
			return err
		}
	}
	return nil
}

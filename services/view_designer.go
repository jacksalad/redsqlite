package services

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/sqliteutil"
)

// 视图设计器（V3.0）：新建/编辑/删除视图。
// SQL 一律由后端生成（不变量 3）：视图名过 identifier 白名单校验，
// body 用 ValidateSingle 拒绝多语句注入且必须以 SELECT/WITH/VALUES 开头；
// 修改视图 = DROP + CREATE 在单事务中原子执行。

// viewBodyRe 从 sqlite_master 的 CREATE VIEW 原文中提取视图名与主体。
// 支持双引号/方括号/反引号/裸标识符四种引用形式。
var viewBodyRe = regexp.MustCompile(
	"(?is)^CREATE\\s+VIEW\\s+(?:\"((?:[^\"]|\"\")+)\"|\\[([^\\]]+)\\]|`([^`]+)`|([A-Za-z_][A-Za-z0-9_$]*))\\s+AS\\s+(.+)$")

// validateViewBody 校验视图主体：单条语句（防注入）且为查询。
// 返回规范化后的主体文本。
func validateViewBody(body string) (string, error) {
	normalized, err := sqliteutil.ValidateSingle(body)
	if err != nil {
		if strings.Contains(err.Error(), "一条") {
			return "", fmt.Errorf("视图主体不能包含多条语句")
		}
		return "", fmt.Errorf("视图主体不能为空")
	}
	switch sqliteutil.FirstKeyword(normalized) {
	case "SELECT", "WITH", "VALUES":
		return normalized, nil
	default:
		return "", fmt.Errorf("视图主体必须是 SELECT / WITH / VALUES 查询语句")
	}
}

// buildCreateViewSQL 生成 CREATE VIEW 语句（视图名已校验）。
func buildCreateViewSQL(spec domain.ViewSpec) (string, string, error) {
	quoted, err := quoteIdent(spec.Name)
	if err != nil {
		return "", "", err
	}
	body, err := validateViewBody(spec.Body)
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("CREATE VIEW %s AS %s", quoted, body), body, nil
}

// GetViewDefinition 返回现有视图的定义（名称 + SELECT 主体），
// 供视图设计器编辑回填。
func (s *SchemaService) GetViewDefinition(connID string, name string) (domain.ViewSpec, error) {
	b := s.bind(connID)
	typ, err := b.schema.ObjectType(name)
	if err != nil {
		return domain.ViewSpec{}, err
	}
	if typ != "view" {
		return domain.ViewSpec{}, fmt.Errorf("对象 %q 不是视图", name)
	}
	ddl, err := b.schema.GetObjectSQL(name)
	if err != nil {
		return domain.ViewSpec{}, err
	}
	body := ddl
	if m := viewBodyRe.FindStringSubmatch(strings.TrimSpace(ddl)); m != nil {
		// 前 4 个捕获组是四种引用形式的视图名，第 5 组是主体；
		// 名称还原真实写法（去双引号转义）
		for i := 1; i <= 4; i++ {
			if m[i] != "" {
				name = strings.ReplaceAll(m[i], "\"\"", "\"")
				break
			}
		}
		body = m[5]
	}
	return domain.ViewSpec{Name: name, Body: body}, nil
}

// PreviewCreateView 预览新建视图。
func (s *SchemaService) PreviewCreateView(connID string, spec domain.ViewSpec) (domain.ViewOpPreview, error) {
	b := s.bind(connID)
	if b.store.ReadOnly(b.connID) {
		return domain.ViewOpPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	sqlText, _, err := buildCreateViewSQL(spec)
	if err != nil {
		return domain.ViewOpPreview{Blocked: err.Error()}, nil
	}
	exists, err := b.schema.NameExists(spec.Name)
	if err != nil {
		return domain.ViewOpPreview{}, err
	}
	if exists {
		return domain.ViewOpPreview{Blocked: fmt.Sprintf("已存在同名对象 %q", spec.Name)}, nil
	}
	return domain.ViewOpPreview{SQL: []string{sqlText}}, nil
}

// ApplyCreateView 执行新建视图，返回已执行的 SQL。
func (s *SchemaService) ApplyCreateView(connID string, spec domain.ViewSpec) (string, error) {
	b := s.bind(connID)
	preview, err := s.PreviewCreateView(connID, spec)
	if err != nil {
		return "", err
	}
	if preview.Blocked != "" {
		return "", fmt.Errorf("%s", preview.Blocked)
	}
	if err := b.schema.Exec(preview.SQL[0]); err != nil {
		slog.Error("新建视图失败", "connID", connID, "view", spec.Name, "err", err)
		return "", fmt.Errorf("新建视图失败: %w", err)
	}
	slog.Info("新建视图完成", "connID", connID, "view", spec.Name)
	return preview.SQL[0], nil
}

// PreviewAlterView 预览修改视图（DROP + CREATE 单事务）。
// OldName 为空按 Name 处理；Name 与 OldName 不同即重命名，
// 引用旧名的视图/触发器将悬空（SQLite 运行时才报错），预览时警告。
func (s *SchemaService) PreviewAlterView(connID string, spec domain.ViewSpec) (domain.ViewOpPreview, error) {
	b := s.bind(connID)
	if b.store.ReadOnly(b.connID) {
		return domain.ViewOpPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	oldName := spec.OldName
	if oldName == "" {
		oldName = spec.Name
	}
	typ, err := b.schema.ObjectType(oldName)
	if err != nil {
		return domain.ViewOpPreview{}, err
	}
	if typ != "view" {
		return domain.ViewOpPreview{Blocked: fmt.Sprintf("对象 %q 不是视图", oldName)}, nil
	}
	sqlText, _, err := buildCreateViewSQL(spec)
	if err != nil {
		return domain.ViewOpPreview{Blocked: err.Error()}, nil
	}
	quotedOld, err := quoteIdent(oldName)
	if err != nil {
		return domain.ViewOpPreview{}, err
	}
	var warnings []string
	if spec.Name != oldName {
		exists, err := b.schema.NameExists(spec.Name)
		if err != nil {
			return domain.ViewOpPreview{}, err
		}
		if exists {
			return domain.ViewOpPreview{Blocked: fmt.Sprintf("已存在同名对象 %q", spec.Name)}, nil
		}
		if refs, err := b.schema.ObjectsReferencing(oldName); err == nil && len(refs) > 0 {
			warnings = append(warnings, "以下视图/触发器引用了原视图名，重命名后将失效（运行时才报错）："+strings.Join(refs, "、"))
		}
	} else if refs, err := b.schema.ObjectsReferencing(spec.Name); err == nil && len(refs) > 0 {
		warnings = append(warnings, "以下视图/触发器引用了该视图，主体变更后可能失效（运行时才报错）："+strings.Join(refs, "、"))
	}
	return domain.ViewOpPreview{
		SQL:      []string{"DROP VIEW " + quotedOld, sqlText},
		Warnings: warnings,
	}, nil
}

// ApplyAlterView 执行视图修改（DROP + CREATE 单事务，任一步失败整体回滚）。
func (s *SchemaService) ApplyAlterView(connID string, spec domain.ViewSpec) error {
	b := s.bind(connID)
	preview, err := s.PreviewAlterView(connID, spec)
	if err != nil {
		return err
	}
	if preview.Blocked != "" {
		return fmt.Errorf("%s", preview.Blocked)
	}
	ctx, err := b.store.Context(connID)
	if err != nil {
		return err
	}
	if err := b.schema.ExecTransaction(ctx, preview.SQL); err != nil {
		slog.Error("修改视图失败", "connID", connID, "view", spec.Name, "err", err)
		return fmt.Errorf("修改视图失败: %w", err)
	}
	slog.Info("修改视图完成", "connID", connID, "view", spec.Name)
	return nil
}

// PreviewDropView 预览删除视图：SQLite 不校验引用，删除后引用它的
// 视图/触发器要到运行时才报错，这里提前给出警告。
func (s *SchemaService) PreviewDropView(connID string, name string) (domain.ViewOpPreview, error) {
	b := s.bind(connID)
	if b.store.ReadOnly(b.connID) {
		return domain.ViewOpPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	typ, err := b.schema.ObjectType(name)
	if err != nil {
		return domain.ViewOpPreview{}, err
	}
	if typ != "view" {
		return domain.ViewOpPreview{Blocked: fmt.Sprintf("对象 %q 不是视图", name)}, nil
	}
	quoted, err := quoteIdent(name)
	if err != nil {
		return domain.ViewOpPreview{}, err
	}
	var warnings []string
	if refs, err := b.schema.ObjectsReferencing(name); err == nil && len(refs) > 0 {
		warnings = append(warnings, "以下视图/触发器引用了该视图，删除后将失效（运行时才报错）："+strings.Join(refs, "、"))
	}
	return domain.ViewOpPreview{SQL: []string{"DROP VIEW " + quoted}, Warnings: warnings}, nil
}

// DropView 删除视图（前端已确认）。
func (s *SchemaService) DropView(connID string, name string) error {
	preview, err := s.PreviewDropView(connID, name)
	if err != nil {
		return err
	}
	if preview.Blocked != "" {
		return fmt.Errorf("%s", preview.Blocked)
	}
	b := s.bind(connID)
	if err := b.schema.Exec(preview.SQL[0]); err != nil {
		slog.Error("删除视图失败", "connID", connID, "view", name, "err", err)
		return fmt.Errorf("删除视图失败: %w", err)
	}
	slog.Info("删除视图", "connID", connID, "view", name)
	return nil
}

package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
	"github.com/redjack/redsqlite/internal/schemadsl"
	"github.com/redjack/redsqlite/repository"
)

// SchemaService 查询表、列、索引、视图、触发器，并提供
// 可视化结构编辑（V2.0）：SQL 由后端生成，前端仅预览与确认。
// V2.1：所有方法以 connID 指定目标连接。
type SchemaService struct {
	store *repository.Store
}

// NewSchemaService 创建结构服务。
func NewSchemaService(store *repository.Store) *SchemaService {
	return &SchemaService{store: store}
}

// bind 返回绑定到单个连接的仓库集合（每次调用构造，无共享状态）。
func (s *SchemaService) bind(connID string) *boundSchema {
	return &boundSchema{
		store:  s.store,
		connID: connID,
		schema: repository.NewSchemaRepository(s.store, connID),
		data:   repository.NewDataRepository(s.store, connID),
	}
}

// GetSchema 返回数据库结构。
func (s *SchemaService) GetSchema(connID string) (domain.SchemaInfo, error) {
	return s.bind(connID).GetSchema()
}

// GetTableColumns 返回表字段。
func (s *SchemaService) GetTableColumns(connID string, tableName string) ([]domain.ColumnInfo, error) {
	return s.bind(connID).GetTableColumns(tableName)
}

// GetTableStructure 返回表完整结构（含可编辑性判断）。
func (s *SchemaService) GetTableStructure(connID string, tableName string) (domain.TableStructure, error) {
	return s.bind(connID).GetTableStructure(tableName)
}

// GetTableForeignKeys 返回表自身定义的外键（V2.3 结构页展示）。
func (s *SchemaService) GetTableForeignKeys(connID string, tableName string) ([]domain.ForeignKeyInfo, error) {
	return s.bind(connID).GetTableForeignKeys(tableName)
}

// GetCompletionSchema 返回 表/视图名 → 列名列表（V2.3 SQL 编辑器补全）。
func (s *SchemaService) GetCompletionSchema(connID string) (map[string][]string, error) {
	return s.bind(connID).CompletionSchema()
}

// PreviewCreateIndex 预览新建索引（V2.3）。Name 为空时自动命名（同名列索引去重后缀）。
func (s *SchemaService) PreviewCreateIndex(connID string, spec domain.CreateIndexSpec) (domain.CreateIndexPreview, error) {
	return s.bind(connID).previewCreateIndex(spec)
}

// ApplyCreateIndex 执行新建索引，返回索引名（V2.3）。
func (s *SchemaService) ApplyCreateIndex(connID string, spec domain.CreateIndexSpec) (string, error) {
	b := s.bind(connID)
	preview, err := b.previewCreateIndex(spec)
	if err != nil {
		return "", err
	}
	if preview.Blocked != "" {
		return "", errors.New(preview.Blocked)
	}
	if err := b.schema.Exec(preview.SQL); err != nil {
		slog.Error("新建索引失败", "connID", connID, "table", spec.Table, "err", err)
		return "", fmt.Errorf("执行失败: %w", err)
	}
	slog.Info("新建索引完成", "connID", connID, "table", spec.Table, "name", preview.Name)
	return preview.Name, nil
}

// PreviewTableOp 预览表结构操作：生成待执行 SQL、阻断原因（非空即禁止执行）与提示。
func (s *SchemaService) PreviewTableOp(connID string, op domain.TableOp) (domain.TableOpPreview, error) {
	return s.bind(connID).buildTableOpPreview(op)
}

// ApplyTableOp 应用表结构操作（重新校验后执行 DDL），返回已执行的 SQL。
func (s *SchemaService) ApplyTableOp(connID string, op domain.TableOp) (string, error) {
	b := s.bind(connID)
	preview, err := b.buildTableOpPreview(op)
	if err != nil {
		return "", err
	}
	if preview.Blocked != "" {
		return "", errors.New(preview.Blocked)
	}
	if err := b.schema.Exec(preview.SQL); err != nil {
		slog.Error("结构变更失败", "connID", connID, "op", op.Kind, "table", op.Table, "err", err)
		return "", fmt.Errorf("执行失败: %w", err)
	}
	slog.Info("结构变更完成", "connID", connID, "op", op.Kind, "table", op.Table)
	return preview.SQL, nil
}

// PreviewCreateTable 预览新建表。
func (s *SchemaService) PreviewCreateTable(connID string, spec domain.CreateTableSpec) (domain.TableOpPreview, error) {
	return s.bind(connID).previewCreateTable(spec)
}

// ApplyCreateTable 执行新建表，返回已执行的 SQL。
func (s *SchemaService) ApplyCreateTable(connID string, spec domain.CreateTableSpec) (string, error) {
	b := s.bind(connID)
	preview, err := b.previewCreateTable(spec)
	if err != nil {
		return "", err
	}
	if preview.Blocked != "" {
		return "", errors.New(preview.Blocked)
	}
	if err := b.schema.Exec(preview.SQL); err != nil {
		slog.Error("新建表失败", "connID", connID, "table", spec.Table, "err", err)
		return "", fmt.Errorf("执行失败: %w", err)
	}
	slog.Info("新建表完成", "connID", connID, "table", spec.Table)
	return preview.SQL, nil
}

// DropTrigger 删除触发器（前端已做确认）。
func (s *SchemaService) DropTrigger(connID string, name string) error {
	b := s.bind(connID)
	if b.store.ReadOnly(connID) {
		return errors.New("数据库以只读方式打开，无法删除触发器")
	}
	typ, err := b.schema.ObjectType(name)
	if err != nil {
		return err
	}
	if typ != "trigger" {
		return fmt.Errorf("对象 %q 不是触发器", name)
	}
	quoted, err := quoteIdent(name)
	if err != nil {
		return err
	}
	if err := b.schema.Exec("DROP TRIGGER " + quoted); err != nil {
		slog.Error("删除触发器失败", "connID", connID, "name", name, "err", err)
		return fmt.Errorf("删除触发器失败: %w", err)
	}
	slog.Info("删除触发器", "connID", connID, "name", name)
	return nil
}

// DropIndex 删除手动创建的索引（约束自动生成的索引不可单独删除）。
func (s *SchemaService) DropIndex(connID string, name string) error {
	b := s.bind(connID)
	if b.store.ReadOnly(connID) {
		return errors.New("数据库以只读方式打开，无法删除索引")
	}
	if strings.HasPrefix(name, "sqlite_") {
		return errors.New("该索引由表约束（主键/唯一）自动创建，需通过修改表结构移除")
	}
	typ, err := b.schema.ObjectType(name)
	if err != nil {
		return err
	}
	if typ != "index" {
		return fmt.Errorf("对象 %q 不是索引", name)
	}
	sqlText, err := b.schema.GetObjectSQL(name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(sqlText) == "" {
		return errors.New("该索引由表约束（主键/唯一）自动创建，需通过修改表结构移除")
	}
	quoted, err := quoteIdent(name)
	if err != nil {
		return err
	}
	if err := b.schema.Exec("DROP INDEX " + quoted); err != nil {
		slog.Error("删除索引失败", "connID", connID, "name", name, "err", err)
		return fmt.Errorf("删除索引失败: %w", err)
	}
	slog.Info("删除索引", "connID", connID, "name", name)
	return nil
}

// ---------- 连接绑定的实现 ----------

// boundSchema 绑定到单个连接的结构查询与编辑逻辑。
type boundSchema struct {
	store  *repository.Store
	connID string
	schema *repository.SchemaRepository
	data   *repository.DataRepository
}

// GetSchema 返回数据库结构。
func (b *boundSchema) GetSchema() (domain.SchemaInfo, error) {
	return b.schema.GetSchema()
}

// GetTableColumns 返回表字段。
func (b *boundSchema) GetTableColumns(tableName string) ([]domain.ColumnInfo, error) {
	return b.schema.GetTableColumns(tableName)
}

// GetTableForeignKeys 返回表自身定义的外键（V2.3）。
func (b *boundSchema) GetTableForeignKeys(tableName string) ([]domain.ForeignKeyInfo, error) {
	return b.schema.GetTableForeignKeys(tableName)
}

// CompletionSchema 返回 表/视图名 → 列名列表（V2.3 SQL 补全）。
func (b *boundSchema) CompletionSchema() (map[string][]string, error) {
	return b.schema.CompletionSchema()
}

// previewCreateIndex 预览新建索引：列存在性校验、自动命名、同名列索引提示。
func (b *boundSchema) previewCreateIndex(spec domain.CreateIndexSpec) (domain.CreateIndexPreview, error) {
	if b.store.ReadOnly(b.connID) {
		return domain.CreateIndexPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	typ, err := b.schema.ObjectType(spec.Table)
	if err != nil {
		return domain.CreateIndexPreview{}, err
	}
	if typ != "table" {
		return domain.CreateIndexPreview{Blocked: fmt.Sprintf("对象 %q 不是表，无法建索引", spec.Table)}, nil
	}
	cols, err := b.schema.GetTableColumns(spec.Table)
	if err != nil {
		return domain.CreateIndexPreview{}, err
	}
	existing := map[string]bool{}
	for _, c := range cols {
		existing[c.Name] = true
	}
	for _, col := range spec.Columns {
		if !existing[col] {
			return domain.CreateIndexPreview{Blocked: fmt.Sprintf("列 %q 不存在于表 %q", col, spec.Table)}, nil
		}
	}

	// 名称解析：显式名称冲突直接阻断；留空则自动命名并去重
	name := strings.TrimSpace(spec.Name)
	if name != "" {
		exists, err := b.schema.NameExists(name)
		if err != nil {
			return domain.CreateIndexPreview{}, err
		}
		if exists {
			return domain.CreateIndexPreview{Blocked: fmt.Sprintf("已存在同名对象 %q", name)}, nil
		}
	} else {
		base := "idx_" + spec.Table + "_" + strings.Join(spec.Columns, "_")
		base = strings.ReplaceAll(base, " ", "_")
		if len(base) > 60 {
			base = base[:60]
		}
		candidate := base
		for i := 2; ; i++ {
			exists, err := b.schema.NameExists(candidate)
			if err != nil {
				return domain.CreateIndexPreview{}, err
			}
			if !exists {
				break
			}
			candidate = fmt.Sprintf("%s_%d", base, i)
			if i > 99 {
				return domain.CreateIndexPreview{Blocked: "自动命名失败：候选名称均被占用，请手动指定索引名"}, nil
			}
		}
		name = candidate
	}

	sqlText, err := schemadsl.BuildCreateIndex(domain.CreateIndexSpec{
		Table: spec.Table, Name: name, Unique: spec.Unique, Columns: spec.Columns,
	})
	if err != nil {
		return domain.CreateIndexPreview{Blocked: err.Error()}, nil
	}

	// 相同列集合的既有索引提示（重复索引通常无意义）
	var warnings []string
	indexes, err := b.schema.GetTableIndexes(spec.Table)
	if err == nil {
		for _, idx := range indexes {
			if sameStringSet(idx.Columns, spec.Columns) {
				if idx.Unique || !spec.Unique {
					warnings = append(warnings, fmt.Sprintf("已存在覆盖相同列的索引 %q，请确认确需重复索引", idx.Name))
				} else {
					warnings = append(warnings, fmt.Sprintf("已存在相同列的非唯一索引 %q，新唯一索引仍有意义", idx.Name))
				}
				break
			}
		}
	}
	return domain.CreateIndexPreview{Name: name, SQL: sqlText, Warnings: warnings}, nil
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	set := map[string]bool{}
	for _, s := range a {
		set[s] = true
	}
	for _, s := range b {
		if !set[s] {
			return false
		}
	}
	return true
}

// GetTableStructure 返回表完整结构（含可编辑性判断）。
func (b *boundSchema) GetTableStructure(tableName string) (domain.TableStructure, error) {
	columns, err := b.schema.GetTableColumns(tableName)
	if err != nil {
		return domain.TableStructure{}, err
	}
	ddl, err := b.schema.GetObjectSQL(tableName)
	if err != nil {
		return domain.TableStructure{}, err
	}
	indexes, err := b.schema.GetTableIndexes(tableName)
	if err != nil {
		return domain.TableStructure{}, err
	}
	isView, err := b.schema.IsView(tableName)
	if err != nil {
		return domain.TableStructure{}, err
	}

	keys, editable, err := b.data.KeyColumns(tableName)
	if err != nil {
		return domain.TableStructure{}, err
	}

	rowCount := int64(0)
	if !isView {
		if result, err := b.data.CountAll(tableName); err == nil {
			rowCount = result
		}
	}

	return domain.TableStructure{
		Name:       tableName,
		Columns:    columns,
		DDL:        ddl,
		Indexes:    indexes,
		RowCount:   rowCount,
		IsView:     isView,
		Editable:   editable && !b.store.ReadOnly(b.connID),
		KeyColumns: keys,
	}, nil
}

// buildTableOpPreview 统一入口：只读/对象类型检查后分派到具体操作。
func (b *boundSchema) buildTableOpPreview(op domain.TableOp) (domain.TableOpPreview, error) {
	if b.store.ReadOnly(b.connID) {
		return domain.TableOpPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	typ, err := b.schema.ObjectType(op.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	if typ != "table" {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("对象 %q 不是表，无法进行结构编辑", op.Table)}, nil
	}

	switch domain.TableOpKind(op.Kind) {
	case domain.OpRenameTable:
		return b.previewRenameTable(op)
	case domain.OpRenameColumn:
		return b.previewRenameColumn(op)
	case domain.OpDropColumn:
		return b.previewDropColumn(op)
	case domain.OpAddColumn:
		return b.previewAddColumn(op)
	case domain.OpDropTable:
		return b.previewDropTable(op)
	default:
		return domain.TableOpPreview{}, fmt.Errorf("不支持的操作类型: %s", op.Kind)
	}
}

func (b *boundSchema) previewRenameTable(op domain.TableOp) (domain.TableOpPreview, error) {
	sqlText, err := schemadsl.BuildAlter(op)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	exists, err := b.schema.NameExists(op.NewName)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	if exists {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("已存在同名对象 %q", op.NewName)}, nil
	}
	var warnings []string
	if refs, err := b.schema.ForeignKeysReferencing(op.Table, ""); err == nil && len(refs) > 0 {
		warnings = append(warnings, "以下外键引用了该表，定义将随重命名自动更新："+fkRefSummary(refs))
	}
	if refs, err := b.schema.ObjectsReferencing(op.Table); err == nil && len(refs) > 0 {
		warnings = append(warnings, "引用该表的视图/触发器定义将自动更新："+strings.Join(refs, "、"))
	}
	return domain.TableOpPreview{SQL: sqlText, Warnings: warnings}, nil
}

func (b *boundSchema) previewRenameColumn(op domain.TableOp) (domain.TableOpPreview, error) {
	sqlText, err := schemadsl.BuildAlter(op)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	cols, err := b.schema.GetTableColumns(op.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	var old, dup bool
	for _, c := range cols {
		if c.Name == op.Column {
			old = true
		}
		if c.Name == op.NewName {
			dup = true
		}
	}
	if !old {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("字段 %q 不存在", op.Column)}, nil
	}
	if dup {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("已存在同名字段 %q", op.NewName)}, nil
	}
	var warnings []string
	if refs, err := b.schema.ObjectsReferencing(op.Column); err == nil && len(refs) > 0 {
		warnings = append(warnings, "引用该字段的索引、视图、触发器定义将自动更新："+strings.Join(refs, "、"))
	}
	return domain.TableOpPreview{SQL: sqlText, Warnings: warnings}, nil
}

func (b *boundSchema) previewDropColumn(op domain.TableOp) (domain.TableOpPreview, error) {
	sqlText, err := schemadsl.BuildAlter(op)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	cols, err := b.schema.GetTableColumns(op.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	var target *domain.ColumnInfo
	for i := range cols {
		if cols[i].Name == op.Column {
			target = &cols[i]
			break
		}
	}
	if target == nil {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("字段 %q 不存在", op.Column)}, nil
	}
	if target.PrimaryKey {
		return domain.TableOpPreview{Blocked: "主键字段不能删除（如需更换主键请新建表迁移）"}, nil
	}
	if len(cols) == 1 {
		return domain.TableOpPreview{Blocked: "表至少要保留一个字段"}, nil
	}

	// 索引覆盖检查（含部分索引 WHERE 子句引用）
	indexes, err := b.schema.GetTableIndexes(op.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	for _, idx := range indexes {
		covered := false
		for _, col := range idx.Columns {
			if col == op.Column {
				covered = true
				break
			}
		}
		if covered {
			return domain.TableOpPreview{Blocked: fmt.Sprintf("字段被索引 %q 使用，请先删除该索引", idx.Name)}, nil
		}
		if !covered && repository.ContainsIdentifier(idx.SQL, op.Column) {
			return domain.TableOpPreview{Blocked: fmt.Sprintf("字段被部分索引 %q 的 WHERE 子句引用，请先删除该索引", idx.Name)}, nil
		}
	}

	// 外键引用检查（父表方向 + 本表子表方向）
	if refs, err := b.schema.ForeignKeysReferencing(op.Table, op.Column); err == nil && len(refs) > 0 {
		return domain.TableOpPreview{Blocked: "字段被外键引用，无法删除：" + fkRefSummary(refs)}, nil
	}

	var warnings []string
	if refs, err := b.schema.ObjectsReferencing(op.Column); err == nil && len(refs) > 0 {
		warnings = append(warnings, "删除后以下视图/触发器的定义可能失效："+strings.Join(refs, "、"))
	}
	return domain.TableOpPreview{SQL: sqlText, Warnings: warnings}, nil
}

func (b *boundSchema) previewAddColumn(op domain.TableOp) (domain.TableOpPreview, error) {
	sqlText, err := schemadsl.BuildAlter(op)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	cols, err := b.schema.GetTableColumns(op.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	for _, c := range cols {
		if op.ColumnSpec != nil && c.Name == op.ColumnSpec.Name {
			return domain.TableOpPreview{Blocked: fmt.Sprintf("已存在同名字段 %q", c.Name)}, nil
		}
	}
	return domain.TableOpPreview{SQL: sqlText}, nil
}

func (b *boundSchema) previewDropTable(op domain.TableOp) (domain.TableOpPreview, error) {
	sqlText, err := schemadsl.BuildAlter(op)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	var warnings []string
	if count, err := b.data.CountAll(op.Table); err == nil && count > 0 {
		warnings = append(warnings, fmt.Sprintf("将删除表中的全部数据（约 %d 行），且无法撤销", count))
	}
	if refs, err := b.schema.ForeignKeysReferencing(op.Table, ""); err == nil && len(refs) > 0 {
		warnings = append(warnings, "以下外键引用了该表，若子表存在引用数据行，删除将失败："+fkRefSummary(refs))
	}
	if refs, err := b.schema.ObjectsReferencing(op.Table); err == nil && len(refs) > 0 {
		warnings = append(warnings, "以下视图/触发器将保留，但定义可能失效："+strings.Join(refs, "、"))
	}
	return domain.TableOpPreview{SQL: sqlText, Warnings: warnings}, nil
}

func (b *boundSchema) previewCreateTable(spec domain.CreateTableSpec) (domain.TableOpPreview, error) {
	if b.store.ReadOnly(b.connID) {
		return domain.TableOpPreview{Blocked: "数据库以只读方式打开，无法修改结构"}, nil
	}
	sqlText, err := schemadsl.BuildCreateTable(spec)
	if err != nil {
		return domain.TableOpPreview{Blocked: err.Error()}, nil
	}
	exists, err := b.schema.NameExists(spec.Table)
	if err != nil {
		return domain.TableOpPreview{}, err
	}
	if exists {
		return domain.TableOpPreview{Blocked: fmt.Sprintf("已存在同名对象 %q", spec.Table)}, nil
	}
	var warnings []string
	hasPK := false
	for _, c := range spec.Columns {
		if c.PrimaryKey {
			hasPK = true
			break
		}
	}
	if !hasPK {
		warnings = append(warnings, "未定义主键：数据行将通过 rowid 定位（仍可正常编辑）")
	}
	return domain.TableOpPreview{SQL: sqlText, Warnings: warnings}, nil
}

// fkRefSummary 汇总外键引用为 "子表.列" 列表（去重，上限 8 条）。
func fkRefSummary(refs []repository.ForeignKeyRef) string {
	seen := map[string]bool{}
	var parts []string
	for _, r := range refs {
		key := r.FromTable + "." + r.FromCol
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, key)
		if len(parts) >= 8 {
			parts = append(parts, "…")
			break
		}
	}
	return strings.Join(parts, "、")
}

// quoteIdent 校验并安全引用标识符（服务层生成 DROP 语句用）。
func quoteIdent(name string) (string, error) {
	quoted, err := identifier.ValidateAndQuote(name)
	if err != nil {
		return "", fmt.Errorf("名称非法: %w", err)
	}
	return quoted, nil
}

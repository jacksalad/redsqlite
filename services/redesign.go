package services

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
	"github.com/redjack/redsqlite/internal/schemadsl"
	"github.com/redjack/redsqlite/repository"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// 表重建（V2.4）：改类型/约束/列序/主键/表名/新增外键。
// 流程：建临时新表 → 拷数据 → 删旧表 → 改名 → 重建索引/触发器，
// 全程单事务，提交前 PRAGMA foreign_key_check 校验，任一步失败整体回滚。
// 外键约束在事务外切换（事务内是 no-op）；单连接保证无并发窗口。

const redesignTempPrefix = "__rs_tmp_"

// RedesignProgressEvent 表重建进度事件名（V3.0）；data 为
// {connId, table, phase, copied, total}，phase: copy=拷贝数据 / finalize=重建索引触发器。
const RedesignProgressEvent = "redesign:progress"

// PreviewTableRedesign 预览表重建：依序语句、提示与阻断原因。
func (s *SchemaService) PreviewTableRedesign(connID string, rd domain.TableRedesign) (domain.RedesignPreview, error) {
	preview, _, err := s.bind(connID).buildRedesign(rd)
	return preview, err
}

// ApplyTableRedesign 执行表重建（重新校验后执行）。数据拷贝按 rowid
// 分块（V3.0），块间通过 RedesignProgressEvent 事件回报进度；
// WITHOUT ROWID 表无法分块，仅发一次总进度。任一步失败整体回滚。
func (s *SchemaService) ApplyTableRedesign(connID string, rd domain.TableRedesign) error {
	b := s.bind(connID)
	preview, plan, err := b.buildRedesign(rd)
	if err != nil {
		return err
	}
	if preview.Blocked != "" {
		return fmt.Errorf("%s", preview.Blocked)
	}

	// 记录初始外键开关，结束时恢复
	fkWasOn := b.schema.ForeignKeysEnabled()
	if err := b.schema.SetForeignKeys(false); err != nil {
		return err
	}
	defer func() {
		if err := b.schema.SetForeignKeys(fkWasOn); err != nil {
			slog.Error("恢复外键约束开关失败", "connID", connID, "err", err)
		}
	}()

	ctx, err := b.store.Context(connID)
	if err != nil {
		return err
	}

	total := int64(0)
	if plan.Copy != nil {
		total = plan.Copy.Total
	}
	lastProgress := int64(0)
	chunk := redesignChunkSize(total)
	emit := func(phase string, copied int64) {
		app := application.Get()
		if app == nil {
			return // 单元测试环境无应用实例
		}
		app.Event.Emit(RedesignProgressEvent, map[string]any{
			"connId": connID, "table": rd.Table,
			"phase": phase, "copied": copied, "total": total,
		})
	}
	if err := b.schema.ExecRedesignPlan(ctx, plan, chunk, func(copied int64) {
		lastProgress = copied
		emit("copy", copied)
	}); err != nil {
		slog.Error("表重建失败", "connID", connID, "table", rd.Table, "err", err)
		return err
	}
	// 数据拷贝完成后进入索引/触发器重建阶段（不可分块，提示前端）
	if plan.Copy != nil && lastProgress > 0 {
		emit("finalize", lastProgress)
	}
	slog.Info("表重建完成", "connID", connID, "table", rd.Table, "newTable", rd.NewTable, "steps", len(preview.SQL))
	return nil
}

// redesignChunkSize 按预计行数自适应分块：total/200（约 200 次进度回调），
// 下限 1000（小表一个块整拷）；repository 侧另钳制上限 50000。
func redesignChunkSize(total int64) int {
	if total <= 0 {
		return 2000
	}
	size := total / 200
	if size < redesignMinChunkHint {
		size = redesignMinChunkHint
	}
	return int(size)
}

const redesignMinChunkHint = 1000

// buildRedesign 校验并生成重建语句序列与执行计划（预览与执行共用）。
func (b *boundSchema) buildRedesign(rd domain.TableRedesign) (domain.RedesignPreview, repository.RedesignPlan, error) {
	plan := repository.RedesignPlan{}
	blocked := func(msg string) (domain.RedesignPreview, repository.RedesignPlan, error) {
		return domain.RedesignPreview{Blocked: msg}, plan, nil
	}
	if b.store.ReadOnly(b.connID) {
		return blocked("数据库以只读方式打开，无法修改结构")
	}
	typ, err := b.schema.ObjectType(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	if typ != "table" {
		return blocked(fmt.Sprintf("对象 %q 不是表，无法重设计", rd.Table))
	}

	// ---------- 收集现状 ----------
	ddl, err := b.schema.GetObjectSQL(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	cols, err := b.schema.GetTableColumns(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	fks, err := b.schema.GetTableForeignKeys(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	indexes, err := b.schema.GetTableIndexesFull(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	triggers, err := b.schema.TableTriggers(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	// 其他表上引用了本表的触发器：RENAME 阶段 SQLite 会重解析全库触发器，
	// 这些触发器在"DROP 旧表 → RENAME 回原名"窗口内悬空 → 必须一并先删后建
	allSchema, err := b.schema.GetSchema()
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	var refTriggers []domain.TriggerInfo
	for _, tr := range allSchema.Triggers {
		if tr.Table == rd.Table {
			continue
		}
		if repository.ContainsIdentifier(tr.SQL, rd.Table) {
			refTriggers = append(refTriggers, tr)
		}
	}
	hasGenerated, err := b.schema.HasGeneratedColumns(rd.Table)
	if err != nil {
		return domain.RedesignPreview{}, plan, err
	}
	withoutRowid := !b.schema.HasRowidColumn(rd.Table)

	// ---------- 保守阻断 ----------
	if hasGenerated {
		return blocked("表含生成列（表达式仅存于建表语句中），暂不支持重建")
	}
	upperDDL := strings.ToUpper(ddl)
	for _, word := range []string{"CHECK", "COLLATE"} {
		if repositoryContainsWord(upperDDL, word) {
			return blocked(fmt.Sprintf("建表语句包含 %s 约束（无法从系统表还原），暂不支持重建", word))
		}
	}

	// ---------- 列清单校验与映射 ----------
	existing := map[string]bool{}
	for _, c := range cols {
		existing[c.Name] = true
	}
	seenOld := map[string]bool{}
	finalNames := map[string]bool{}
	renamed := map[string]string{} // 旧名 -> 新名
	dropped := map[string]bool{}
	newCols := 0
	for _, rc := range rd.Columns {
		if rc.Name != "" {
			if !existing[rc.Name] {
				return blocked(fmt.Sprintf("列 %q 不存在于表中", rc.Name))
			}
			if seenOld[rc.Name] {
				return blocked(fmt.Sprintf("列 %q 在清单中重复出现", rc.Name))
			}
			seenOld[rc.Name] = true
		} else {
			if strings.TrimSpace(rc.NewName) == "" {
				return blocked("新增列必须填写新列名")
			}
			newCols++
		}
		final := rc.NewName
		if final == "" {
			final = rc.Name
		}
		if finalNames[final] {
			return blocked(fmt.Sprintf("新列名 %q 重复", final))
		}
		finalNames[final] = true
		if rc.Drop {
			if rc.Name == "" {
				return blocked("新增列不能标记删除")
			}
			dropped[rc.Name] = true
		} else if rc.Name != "" && rc.NewName != "" && rc.NewName != rc.Name {
			renamed[rc.Name] = rc.NewName
		}
	}
	var missing []string
	for _, c := range cols {
		if !seenOld[c.Name] {
			missing = append(missing, c.Name)
		}
	}
	if len(missing) > 0 {
		return blocked("列清单必须覆盖全部现有字段，缺少：" + strings.Join(missing, "、"))
	}
	surviveCount := len(cols) - len(dropped) + newCols
	if surviveCount == 0 {
		return blocked("表至少要保留一个字段")
	}
	anyColumnChange := len(renamed) > 0 || len(dropped) > 0

	// 新表名
	finalTable := strings.TrimSpace(rd.NewTable)
	if finalTable == "" {
		finalTable = rd.Table
	}
	if finalTable != rd.Table {
		exists, err := b.schema.NameExists(finalTable)
		if err != nil {
			return domain.RedesignPreview{}, plan, err
		}
		if exists {
			return blocked(fmt.Sprintf("已存在同名对象 %q", finalTable))
		}
		if len(triggers) > 0 {
			return blocked("表上存在触发器时不支持同时重命名表（请先删除触发器或去掉重命名）")
		}
		// 其他表的外键引用不会被重写，重建后的外键检查必然失败 → 直接阻断
		if refs, err := b.schema.ForeignKeysReferencing(rd.Table, ""); err == nil && len(refs) > 0 {
			return blocked("其他表通过外键引用该表（" + fkRefSummary(refs) + "），不支持重命名")
		}
		// 任意触发器引用旧表名：重命名后按原文重建必然失败；视图悬空会破坏后续
		// 其他 ALTER RENAME 的重解析 → 一并阻断（此处 allSchema 已在上面加载）
		var triggerRefs, viewRefs []string
		for _, tr := range allSchema.Triggers {
			if repository.ContainsIdentifier(tr.SQL, rd.Table) {
				triggerRefs = append(triggerRefs, tr.Name)
			}
		}
		for _, v := range allSchema.Views {
			if repository.ContainsIdentifier(v.SQL, rd.Table) {
				viewRefs = append(viewRefs, v.Name)
			}
		}
		if len(triggerRefs) > 0 || len(viewRefs) > 0 {
			parts := append(triggerRefs, viewRefs...)
			return blocked("以下触发器/视图引用了该表，重命名将使其失效（请先调整）：" + strings.Join(parts, "、"))
		}
	}

	// 主键：新设计中的主键列
	var pkCols []domain.RedesignColumn
	for _, rc := range rd.Columns {
		if rc.PrimaryKey && !rc.Drop {
			pkCols = append(pkCols, rc)
		}
	}
	if withoutRowid && len(pkCols) == 0 {
		return blocked("WITHOUT ROWID 表必须保留主键")
	}
	hadAutoincrement := repositoryContainsWord(upperDDL, "AUTOINCREMENT")
	keepAutoincrement := hadAutoincrement && len(pkCols) == 1 &&
		strings.EqualFold(strings.TrimSpace(pkCols[0].Type), "INTEGER")

	// ---------- 生成新表 CREATE ----------
	specCols := make([]domain.ColumnSpec, 0, surviveCount)
	for _, rc := range rd.Columns {
		if rc.Drop {
			continue
		}
		name := rc.NewName
		if name == "" {
			name = rc.Name
		}
		if rc.Name == "" && rc.NotNull {
			// 新增 NOT NULL 列必须有非 NULL 默认值（拷贝阶段靠默认值填充）
			d := strings.TrimSpace(rc.Default)
			if d == "" || (!rc.DefaultIsExpression && strings.EqualFold(d, "NULL")) {
				return blocked(fmt.Sprintf("新增的 NOT NULL 列 %q 必须提供非 NULL 默认值", name))
			}
		}
		specCols = append(specCols, domain.ColumnSpec{
			Name:                name,
			Type:                rc.Type,
			NotNull:             rc.NotNull,
			PrimaryKey:          rc.PrimaryKey,
			AutoIncrement:       rc.PrimaryKey && keepAutoincrement,
			Default:             rc.Default,
			DefaultIsExpression: rc.DefaultIsExpression,
		})
	}

	// 外键：现有（单列映射改名；复合按原文重建，V3.0）+ 新增
	fkSpecs := make([]domain.ForeignKeySpec, 0, len(fks))
	fkByID := map[int][]domain.ForeignKeyInfo{}
	for _, fk := range fks {
		fkByID[fk.ID] = append(fkByID[fk.ID], fk)
	}
	for id := 0; id <= maxFKID(fkByID); id++ {
		group := fkByID[id]
		if len(group) == 0 {
			continue
		}
		onDelete := group[0].OnDelete
		if onDelete == "" || strings.EqualFold(onDelete, "NO ACTION") {
			onDelete = ""
		}
		onUpdate := group[0].OnUpdate
		if onUpdate == "" || strings.EqualFold(onUpdate, "NO ACTION") {
			onUpdate = ""
		}
		if len(group) > 1 {
			// 复合外键（V3.0）：按 (id, seq) 聚合的列序重建表级 FOREIGN KEY；
			// 涉及删除/改名列仍阻断（复合列映射无法从系统表可靠还原）
			sort.Slice(group, func(i, j int) bool { return group[i].Seq < group[j].Seq })
			cols := make([]string, 0, len(group))
			refCols := make([]string, 0, len(group))
			for _, fk := range group {
				if dropped[fk.Column] {
					return blocked(fmt.Sprintf("复合外键（引用 %q）的列 %q 不能删除，请先通过 SQL 移除该外键", fk.RefTable, fk.Column))
				}
				if _, isRenamed := renamed[fk.Column]; isRenamed {
					return blocked(fmt.Sprintf("复合外键（引用 %q）涉及改名列 %q，暂不支持（请先通过 SQL 处理）", fk.RefTable, fk.Column))
				}
				cols = append(cols, fk.Column)
				refCols = append(refCols, fk.RefColumn)
			}
			// RefColumns 任一为空 → 引用父表主键，省略被引用列清单
			for _, rc := range refCols {
				if rc == "" {
					refCols = nil
					break
				}
			}
			fkSpecs = append(fkSpecs, domain.ForeignKeySpec{
				RefTable: group[0].RefTable, OnDelete: onDelete, OnUpdate: onUpdate,
				Columns: cols, RefColumns: refCols,
			})
			continue
		}
		fk := group[0]
		if dropped[fk.Column] {
			return blocked(fmt.Sprintf("外键列 %q 不能删除", fk.Column))
		}
		mapped := fk.Column
		if n, ok := renamed[fk.Column]; ok {
			mapped = n
		}
		fkSpecs = append(fkSpecs, domain.ForeignKeySpec{
			Column: mapped, RefTable: fk.RefTable, RefColumn: fk.RefColumn,
			OnDelete: onDelete, OnUpdate: onUpdate,
		})
	}
	for _, fk := range rd.AddForeignKeys {
		if !finalNames[fk.Column] {
			return blocked(fmt.Sprintf("新增外键的列 %q 不在新表字段中", fk.Column))
		}
		fkSpecs = append(fkSpecs, fk)
	}

	// 临时表名（碰撞时加后缀）
	tempName := redesignTempPrefix + rd.Table
	if len(tempName) > 100 {
		tempName = tempName[:100]
	}
	for i := 2; ; i++ {
		exists, err := b.schema.NameExists(tempName)
		if err != nil {
			return domain.RedesignPreview{}, plan, err
		}
		if !exists {
			break
		}
		tempName = fmt.Sprintf("%s_%d", redesignTempPrefix+rd.Table[:min(90, len(rd.Table))], i)
	}

	createSQL, err := schemadsl.BuildCreateTable(domain.CreateTableSpec{
		Table: tempName, Columns: specCols, ForeignKeys: fkSpecs, WithoutRowid: withoutRowid,
	})
	if err != nil {
		return blocked(err.Error())
	}

	// ---------- 拷数据 ----------
	var insertCols, selectCols []string
	for _, rc := range rd.Columns {
		if rc.Drop || rc.Name == "" {
			continue
		}
		qNew, err1 := identifier.ValidateAndQuote(nameOr(rc.NewName, rc.Name))
		qOld, err2 := identifier.ValidateAndQuote(rc.Name)
		if err1 != nil || err2 != nil {
			return blocked("列名非法")
		}
		insertCols = append(insertCols, qNew)
		selectCols = append(selectCols, qOld)
	}
	tableQ, err := identifier.ValidateAndQuote(rd.Table)
	if err != nil {
		return blocked("表名非法")
	}
	tempQ, err := identifier.ValidateAndQuote(tempName)
	if err != nil {
		return blocked("临时表名非法")
	}

	copySQL := ""
	if len(insertCols) > 0 {
		copySQL = fmt.Sprintf("INSERT INTO %s (%s) SELECT %s FROM %s",
			tempQ, strings.Join(insertCols, ", "), strings.Join(selectCols, ", "), tableQ)
	} else {
		// 全部为新增列：仅建空表
		copySQL = fmt.Sprintf("INSERT INTO %s DEFAULT VALUES", tempQ)
	}

	// 执行计划（V3.0）：拷数据独立成步骤，rowid 表按块执行并回报进度
	plan.Pre = []string{createSQL}
	if len(insertCols) > 0 {
		plan.Copy = &repository.RedesignCopyStep{
			Target:     tempQ,
			TargetCols: insertCols,
			Source:     tableQ,
			SourceCols: selectCols,
			ByRowid:    !withoutRowid,
		}
		plan.Copy.Total, _ = b.data.CountAll(rd.Table)
	} else {
		plan.Pre = append(plan.Pre, copySQL)
	}

	finalQ, err := identifier.ValidateAndQuote(finalTable)
	if err != nil {
		return blocked("目标表名非法")
	}

	// 官方 12 步流程：先删本表触发器再删表——否则 RENAME 阶段 SQLite
	// 重解析全部触发器时会因悬空引用失败（引用本表的其他表触发器同理）
	statements := []string{createSQL, copySQL}
	droppedTriggerNames := map[string]bool{}
	for _, tr := range append(append([]domain.TriggerInfo{}, triggers...), refTriggers...) {
		if droppedTriggerNames[tr.Name] {
			continue
		}
		droppedTriggerNames[tr.Name] = true
		statements = append(statements, "DROP TRIGGER "+identifier.Quote(tr.Name))
	}
	statements = append(statements,
		fmt.Sprintf("DROP TABLE %s", tableQ),
		fmt.Sprintf("ALTER TABLE %s RENAME TO %s", tempQ, finalQ),
	)

	// ---------- 重建索引 ----------
	var warnings []string
	usedNames := map[string]bool{finalTable: true}
	for _, idx := range indexes {
		if idx.Origin == "pk" {
			continue // WITHOUT ROWID 主键索引随表重建
		}
		if idx.Origin == "c" {
			if idx.Partial || idx.HasExpression {
				if anyColumnChange {
					return blocked(fmt.Sprintf(
						"索引 %q 是部分索引/表达式索引，且本次涉及列改名或删除，无法自动重建（请先删除该索引或去掉相关改动）", idx.Name))
				}
				statements = append(statements, idx.SQL) // 原文重建
				continue
			}
			// 普通索引：按映射后的列重生成（支持改名）
			qCols := make([]string, 0, len(idx.Cols))
			ok := true
			for _, c := range idx.Cols {
				mapped := c
				if n, isRenamed := renamed[c]; isRenamed {
					mapped = n
				}
				if !finalNames[mapped] {
					ok = false
					break
				}
				q, err := identifier.ValidateAndQuote(mapped)
				if err != nil {
					ok = false
					break
				}
				qCols = append(qCols, q)
			}
			if !ok {
				warnings = append(warnings, fmt.Sprintf("索引 %q 引用了被删除的列，将不再保留", idx.Name))
				continue
			}
			unique := ""
			if idx.Unique {
				unique = "UNIQUE "
			}
			statements = append(statements, fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
				unique, identifier.Quote(idx.Name), finalQ, strings.Join(qCols, ", ")))
			continue
		}
		if idx.Origin == "u" {
			if idx.Partial {
				return blocked(fmt.Sprintf("唯一约束 %q 带部分索引 WHERE 子句，无法自动还原，暂不支持重建", idx.Name))
			}
			// 唯一约束 → 重生成为命名唯一索引
			qCols := make([]string, 0, len(idx.Cols))
			ok := true
			for _, c := range idx.Cols {
				mapped := c
				if n, isRenamed := renamed[c]; isRenamed {
					mapped = n
				}
				if !finalNames[mapped] {
					ok = false
					break
				}
				q, err := identifier.ValidateAndQuote(mapped)
				if err != nil {
					ok = false
					break
				}
				qCols = append(qCols, q)
			}
			if !ok {
				return blocked(fmt.Sprintf("唯一约束 %q 覆盖的列被删除，如需删除请先在 SQL 页手动处理", idx.Name))
			}
			name := uniqueIndexName(finalTable, idx.Cols)
			for i := 2; ; i++ {
				exists, err := b.schema.NameExists(name)
				if err != nil {
					return domain.RedesignPreview{}, plan, err
				}
				if !exists && !usedNames[name] {
					break
				}
				name = fmt.Sprintf("%s_%d", uniqueIndexName(finalTable, idx.Cols), i)
			}
			usedNames[name] = true
			statements = append(statements, fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
				identifier.Quote(name), finalQ, strings.Join(qCols, ", ")))
			warnings = append(warnings, fmt.Sprintf("唯一约束将重建为唯一索引 %s", name))
		}
	}

	// ---------- 重建触发器（本表 + 引用本表的） ----------
	for _, tr := range append(append([]domain.TriggerInfo{}, triggers...), refTriggers...) {
		if !droppedTriggerNames[tr.Name] {
			continue
		}
		for old := range renamed {
			if repository.ContainsIdentifier(tr.SQL, old) {
				return blocked(fmt.Sprintf("触发器 %q 引用了改名的列 %q，请先删除该触发器", tr.Name, old))
			}
		}
		for old := range dropped {
			if repository.ContainsIdentifier(tr.SQL, old) {
				return blocked(fmt.Sprintf("触发器 %q 引用了删除的列 %q，请先删除该触发器", tr.Name, old))
			}
		}
		if strings.TrimSpace(tr.SQL) != "" {
			statements = append(statements, tr.SQL)
		}
	}

	// ---------- 提示 ----------
	if plan.Copy != nil && plan.Copy.Total > 0 {
		warnings = append(warnings, fmt.Sprintf("将复制全部 %d 行数据，大表可能耗时较长（期间数据库保持一致）", plan.Copy.Total))
	}
	if hadAutoincrement {
		warnings = append(warnings, "AUTOINCREMENT 自增计数将重置（从当前最大值重新计）")
	}
	if len(pkCols) > 0 && pkChanged(cols, rd) {
		warnings = append(warnings, "主键变更后如存在重复值或 NULL，重建将失败并整体回滚")
	}
	if notNullTightened(cols, rd) {
		warnings = append(warnings, "收紧 NOT NULL 后如现有数据含 NULL，重建将失败并整体回滚")
	}
	if typeChanged(cols, rd) {
		warnings = append(warnings, "类型变更将按新类型亲和性转换现有数据（可能丢失精度）")
	}
	for old := range dropped {
		if refs, err := b.schema.ObjectsReferencing(old); err == nil && len(refs) > 0 {
			warnings = append(warnings, fmt.Sprintf("删除列 %s 后以下视图/触发器可能失效：%s", old, strings.Join(refs, "、")))
		}
	}

	// 执行计划的 Post = 建表/拷数据之后的所有语句（statements 前 2 条
	// 是 createSQL 与 copySQL，已进 Pre/Copy）
	plan.Post = statements[2:]
	if plan.Copy != nil && plan.Copy.Total > 100000 {
		warnings = append(warnings, fmt.Sprintf(
			"将分块复制约 %d 行数据并实时回报进度（期间数据库保持一致）", plan.Copy.Total))
	}

	return domain.RedesignPreview{SQL: statements, Warnings: warnings}, plan, nil
}

// repositoryContainsWord 判断大写 DDL 中是否出现指定大写词（词边界匹配）。
func repositoryContainsWord(upperText, upperWord string) bool {
	return repository.ContainsIdentifier(upperText, upperWord)
}

func nameOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func maxFKID(byID map[int][]domain.ForeignKeyInfo) int {
	highest := 0
	for id := range byID {
		if id > highest {
			highest = id
		}
	}
	return highest
}

func uniqueIndexName(table string, cols []string) string {
	base := "uq_" + table + "_" + strings.Join(cols, "_")
	base = strings.ReplaceAll(base, " ", "_")
	if len(base) > 60 {
		base = base[:60]
	}
	return base
}

func pkChanged(cols []domain.ColumnInfo, rd domain.TableRedesign) bool {
	old := map[string]bool{}
	for _, c := range cols {
		if c.PrimaryKey {
			old[c.Name] = true
		}
	}
	for _, rc := range rd.Columns {
		if rc.Name == "" {
			continue
		}
		if rc.PrimaryKey != old[rc.Name] {
			return true
		}
	}
	return false
}

func notNullTightened(cols []domain.ColumnInfo, rd domain.TableRedesign) bool {
	old := map[string]bool{}
	for _, c := range cols {
		old[c.Name] = c.NotNull
	}
	for _, rc := range rd.Columns {
		if rc.Name == "" {
			continue
		}
		if rc.NotNull && !old[rc.Name] {
			return true
		}
	}
	return false
}

func typeChanged(cols []domain.ColumnInfo, rd domain.TableRedesign) bool {
	old := map[string]string{}
	for _, c := range cols {
		old[c.Name] = strings.ToUpper(strings.TrimSpace(c.Type))
	}
	for _, rc := range rd.Columns {
		if rc.Name == "" {
			continue
		}
		if strings.ToUpper(strings.TrimSpace(rc.Type)) != old[rc.Name] {
			return true
		}
	}
	return false
}

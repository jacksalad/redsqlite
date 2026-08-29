package services

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/redjack/redsqlite/domain"
	"github.com/redjack/redsqlite/internal/identifier"
	"github.com/redjack/redsqlite/repository"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// TransferService 跨连接数据传输（V3.0）：在两个已打开连接之间复制表
// （仅结构 / 结构+数据，索引触发器默认连带）。两个连接各是独立 *sql.DB，
// 事务各自开，绝无跨库事务；目标侧失败整体回滚，源侧只读不受影响。
type TransferService struct {
	store *repository.Store
}

// NewTransferService 创建传输服务。
func NewTransferService(store *repository.Store) *TransferService {
	return &TransferService{store: store}
}

// TransferProgressEvent 传输进度事件名；data 为
// {targetConnId, targetTable, phase, copied, total}，phase: copy / finalize。
const TransferProgressEvent = "transfer:progress"

// validate 校验请求基本合法性，返回规范化后的请求。
// 源/目标必须是两个不同连接：同连接（同一 *sql.DB）下目标事务会占住
// 唯一连接（SetMaxOpenConns(1)），读源数据将死锁；同库复制表请走 SQL 页。
func (s *TransferService) validate(req domain.TransferRequest) (domain.TransferRequest, error) {
	req.SourceTable = strings.TrimSpace(req.SourceTable)
	req.TargetTable = strings.TrimSpace(req.TargetTable)
	if req.SourceConnID == "" || req.TargetConnID == "" {
		return req, fmt.Errorf("缺少源/目标连接")
	}
	if req.SourceConnID == req.TargetConnID {
		return req, fmt.Errorf("源与目标是同一连接：同库复制表请在 SQL 查询页执行 INSERT INTO ... SELECT")
	}
	if _, err := s.store.Context(req.SourceConnID); err != nil {
		return req, fmt.Errorf("源连接未打开")
	}
	if _, err := s.store.Context(req.TargetConnID); err != nil {
		return req, fmt.Errorf("目标连接未打开")
	}
	if req.SourceTable == "" || req.TargetTable == "" {
		return req, fmt.Errorf("缺少源表/目标表名")
	}
	if _, err := identifier.ValidateAndQuote(req.SourceTable); err != nil {
		return req, fmt.Errorf("源表名非法: %w", err)
	}
	if _, err := identifier.ValidateAndQuote(req.TargetTable); err != nil {
		return req, fmt.Errorf("目标表名非法: %w", err)
	}
	return req, nil
}

// PreviewTransfer 预检传输：读源 DDL 原文、行数预估、目标同名冲突与依赖提示。
func (s *TransferService) PreviewTransfer(req domain.TransferRequest) (domain.TransferPreview, error) {
	req, err := s.validate(req)
	if err != nil {
		return domain.TransferPreview{}, err
	}
	eng := repository.NewTransferRepository(s.store, req.SourceConnID, req.TargetConnID)
	ctx, err := s.store.Context(req.SourceConnID)
	if err != nil {
		return domain.TransferPreview{}, err
	}
	preview, err := eng.Preview(ctx, req.SourceTable)
	if err != nil {
		return domain.TransferPreview{}, err
	}
	preview.TargetTable = req.TargetTable

	targetSch := repository.NewSchemaRepository(s.store, req.TargetConnID)
	if s.store.ReadOnly(req.TargetConnID) {
		preview.Blocked = "目标连接以只读方式打开，无法写入"
		return preview, nil
	}
	if exists, err := targetSch.NameExists(req.TargetTable); err != nil {
		return domain.TransferPreview{}, err
	} else if exists {
		preview.Blocked = fmt.Sprintf("目标库已存在同名对象 %q（不支持覆盖，请更换目标表名）", req.TargetTable)
		return preview, nil
	}

	preview.Warnings = s.transferWarnings(eng, req, preview)
	return preview, nil
}

// transferWarnings 汇总依赖与行为提示（外键父表缺失、rowid 不保留等）。
func (s *TransferService) transferWarnings(eng *repository.TransferRepository, req domain.TransferRequest, preview domain.TransferPreview) []string {
	var warnings []string
	// 外键：引用的父表若不在目标库，建表成功但写入数据会失败（外键约束）
	fks, err := repository.NewSchemaRepository(s.store, req.SourceConnID).GetTableForeignKeys(req.SourceTable)
	if err == nil && len(fks) > 0 {
		missing := map[string]bool{}
		for _, fk := range fks {
			missing[fk.RefTable] = true
		}
		var missingList []string
		for table := range missing {
			exists, _ := repository.NewSchemaRepository(s.store, req.TargetConnID).NameExists(table)
			if !exists {
				missingList = append(missingList, table)
			}
		}
		if len(missingList) > 0 {
			sort.Strings(missingList)
			warnings = append(warnings, "外键引用的表在目标库不存在，复制数据将失败（请先传输被引用表）："+strings.Join(missingList, "、"))
		}
	}
	if req.IncludeData && preview.RowCount > 0 {
		if strings.Contains(strings.ToUpper(preview.SourceDDL), "AUTOINCREMENT") {
			warnings = append(warnings, "AUTOINCREMENT 自增计数不随传输复制（目标表按当前最大值重新计）")
		}
		warnings = append(warnings, "无显式主键表的 rowid 不保留（目标侧行自动分配新 rowid）")
	}
	if req.IncludeTriggers && len(preview.TriggerDDLs) > 0 {
		warnings = append(warnings, "触发器将按原文重建：其引用的其他表若不在目标库，建触发器仍会成功，但相应数据操作将失败")
	}
	return warnings
}

// ApplyTransfer 执行传输（重新预检后执行）。目标侧 foreign_keys 在事务外
// 关闭（事务内是 no-op），结束后恢复；任一步失败目标侧整体回滚。
func (s *TransferService) ApplyTransfer(req domain.TransferRequest) (domain.TransferResult, error) {
	start := time.Now()
	req, err := s.validate(req)
	if err != nil {
		return domain.TransferResult{}, err
	}
	preview, err := s.PreviewTransfer(req)
	if err != nil {
		return domain.TransferResult{}, err
	}
	if preview.Blocked != "" {
		return domain.TransferResult{}, fmt.Errorf("%s", preview.Blocked)
	}

	eng := repository.NewTransferRepository(s.store, req.SourceConnID, req.TargetConnID)
	ctx, err := s.store.Context(req.TargetConnID)
	if err != nil {
		return domain.TransferResult{}, err
	}

	// 目标侧外键约束事务外切换（沿用表重建经验），单连接无并发窗口
	targetSch := repository.NewSchemaRepository(s.store, req.TargetConnID)
	fkWasOn := targetSch.ForeignKeysEnabled()
	if err := targetSch.SetForeignKeys(false); err != nil {
		return domain.TransferResult{}, err
	}
	defer func() {
		if err := targetSch.SetForeignKeys(fkWasOn); err != nil {
			slog.Error("恢复目标连接外键约束失败", "connID", req.TargetConnID, "err", err)
		}
	}()

	total := preview.RowCount
	emit := func(phase string, copied int64) {
		app := application.Get()
		if app == nil {
			return // 单元测试环境无应用实例
		}
		app.Event.Emit(TransferProgressEvent, map[string]any{
			"targetConnId": req.TargetConnID, "targetTable": req.TargetTable,
			"phase": phase, "copied": copied, "total": total,
		})
	}

	copied, err := eng.Apply(ctx, req, emit)
	if err != nil {
		slog.Error("跨连接传输失败", "source", req.SourceConnID+"."+req.SourceTable,
			"target", req.TargetConnID+"."+req.TargetTable, "err", err)
		return domain.TransferResult{}, err
	}
	slog.Info("跨连接传输完成", "source", req.SourceConnID+"."+req.SourceTable,
		"target", req.TargetConnID+"."+req.TargetTable, "rows", copied)
	return domain.TransferResult{RowsCopied: copied, ElapsedMs: time.Since(start).Milliseconds()}, nil
}

package repository

import "errors"

// ErrNotOpen 连接不存在或已关闭（V2.1 多连接：按 connID 查找失败）。
var ErrNotOpen = errors.New("数据库连接不存在或已关闭")

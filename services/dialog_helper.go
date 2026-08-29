package services

import (
	"github.com/wailsapp/wails/v3/pkg/application"
)

func applicationGet() *application.App {
	return application.Get()
}

var errApplicationNotReady = &simpleError{"应用尚未初始化"}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

func wrapDialogError(err error) error {
	return &simpleError{"打开对话框失败: " + err.Error()}
}

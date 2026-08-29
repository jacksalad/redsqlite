package main

import (
	"embed"
	"fmt"
	"log"
	"log/slog"

	"github.com/redjack/redsqlite/internal/appdata"
	"github.com/redjack/redsqlite/internal/logging"
	"github.com/redjack/redsqlite/repository"
	"github.com/redjack/redsqlite/services"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// appVersion 应用版本，生产构建由 Taskfile 的 APP_VERSION 经 -ldflags -X 注入。
var appVersion = "dev"

func main() {
	closer, logErr := logging.Setup(appVersion)
	defer closer()
	if logErr != nil {
		slog.Error("日志初始化降级为仅控制台输出", "err", logErr)
	}

	apps, err := appdata.New()
	if err != nil {
		slog.Error("初始化配置存储失败", "err", err)
		log.Fatalf("初始化配置存储失败: %v", err)
	}

	store := repository.NewStore()

	app := application.New(application.Options{
		Name:        "RedSQLite",
		Description: "A lightweight SQLite desktop client",
		Services: []application.Service{
			application.NewService(services.NewDatabaseService(store, apps)),
			application.NewService(services.NewSchemaService(store)),
			application.NewService(services.NewDataService(store)),
			application.NewService(services.NewQueryService(store, apps)),
			application.NewService(services.NewExportService(store)),
			application.NewService(services.NewImportService(store)),
			application.NewService(services.NewFileService()),
			application.NewService(services.NewRecentService(apps)),
			application.NewService(services.NewSettingsService(apps)),
			application.NewService(services.NewTransferService(store)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "RedSQLite",
		Width:            1280,
		Height:           800,
		MinWidth:         960,
		MinHeight:        600,
		BackgroundColour: application.NewRGB(24, 26, 32),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		fmt.Println(err)
		slog.Error("应用退出异常", "err", err)
		log.Fatal(err)
	}
}

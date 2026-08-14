// Command OpenIPReporter is the desktop application: a window that listens
// for miner IP reports and records the physical position of each one.
//
// The rules that keep the data correct live in openipreporter/internal/walk
// and are tested without any UI in the loop. This package is the window around
// them.
package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set at build time by the release workflow, matching the tag.
var version = "dev"

func main() {
	app := NewApp()
	app.version = version

	err := wails.Run(&options.App{
		Title:  "OpenIPReporter",
		Width:  1180,
		Height: 820,
		// A rack walk is a lot of rows; below this the table stops being
		// readable at arm's length.
		MinWidth:  900,
		MinHeight: 620,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []any{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
		},
		Mac: &mac.Options{
			TitleBar: mac.TitleBarHiddenInset(),
		},
	})
	if err != nil {
		panic(err)
	}
}

package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            "Moon Bridge Manager",
		Width:            1200,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		DisableResize:    false,
		Frameless:        false,
		EnableDefaultContextMenu: true,
		HideWindowOnClose: true,
		BackgroundColour: &options.RGBA{R: 249, G: 250, B: 251, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

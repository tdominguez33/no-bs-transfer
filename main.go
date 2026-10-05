package main

import (
	"embed"

	"no-bs-transfer/pkg/config"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// Create an instance of the app structure
	app := NewApp()

	// Load configuration to restore saved window size
	cfg := config.Load()
	width := cfg.WindowWidth
	height := cfg.WindowHeight
	if width < 450 {
		width = 960
	}
	if height < 300 {
		height = 720
	}

	// Create application with options
	err := wails.Run(&options.App{
		Title:     "no-bs-transfer",
		Width:     width,
		Height:    height,
		MinWidth:  450,
		MinHeight: 300,
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 2, G: 6, B: 23, A: 1}, // Slate-950
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		OnBeforeClose:    app.beforeClose,
		Bind: []interface{}{
			app,
		},
		DragAndDrop: &options.DragAndDrop{
			EnableFileDrop:     true,
			DisableWebViewDrop: true,
		},
		Windows: &windows.Options{
			DisableFramelessWindowDecorations: false,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "ManticoreSearch GUI",
		Description: "A desktop client for Manticore Search",
		Services: []application.Service{
			application.NewService(&ConnectionService{}),
			application.NewService(&QueryService{}),
			application.NewService(&TableService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:     "ManticoreSearch GUI",
		Width:     1380,
		Height:    860,
		MinWidth:  1000,
		MinHeight: 620,
		Frameless: true,
		URL:       "/",
		BackgroundColour: application.NewRGB(13, 15, 22),
	})

	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"embed"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
)

//go:embed assets/*
var embeddedAssets embed.FS

//go:embed assets/tray.png
var trayIcon []byte

//go:embed build/appicon.png
var appIcon []byte

func main() {
	configPath := defaultSSHConfigPath()
	storePath, err := defaultStorePath()
	if err != nil {
		log.Fatal(err)
	}

	manager, err := NewForwardManager(storePath, "ssh")
	if err != nil {
		log.Fatal(err)
	}

	service := NewForwardService(configPath, manager)
	var popup *application.WebviewWindow

	app := application.New(application.Options{
		Name:        "SSH Port Forward",
		Description: "Manage SSH port forwarding from a system tray popup.",
		Icon:        appIcon,
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(embeddedAssets),
		},
		Services: []application.Service{
			application.NewService(service),
		},
		Mac: application.MacOptions{
			ActivationPolicy: application.ActivationPolicyAccessory,
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "local.ssh-port-forward.tray",
			OnSecondInstanceLaunch: func(_ application.SecondInstanceData) {
				if popup != nil {
					popup.Show().Focus()
				}
			},
		},
	})

	popup = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "SSH Port Forward",
		Width:            460,
		Height:           680,
		MinWidth:         400,
		MinHeight:        560,
		AlwaysOnTop:      true,
		Frameless:        true,
		Hidden:           true,
		DisableResize:    true,
		HideOnFocusLost:  true,
		HideOnEscape:     true,
		BackgroundColour: application.NewRGB(10, 13, 18),
		Windows: application.WindowsWindow{
			Theme:                  application.Dark,
			HiddenOnTaskbar:        true,
			DisableMenu:            true,
			NonClientRegionSupport: true,
		},
	})
	popup.SetURL("/")
	popup.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		popup.Hide()
		e.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetTooltip("SSH Port Forward")
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(icons.SystrayMacTemplate)
	} else {
		tray.SetIcon(trayIcon)
	}

	menu := app.Menu.New()
	menu.Add("刷新 Host 列表").OnClick(func(_ *application.Context) {
		tray.ShowWindow()
		popup.ExecJS("window.sshForwardRefresh?.()")
	})
	menu.Add("隐藏窗口").OnClick(func(_ *application.Context) {
		tray.HideWindow()
	})
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(_ *application.Context) {
		app.Quit()
	})

	tray.AttachWindow(popup).WindowOffset(6).WindowDebounce(250).SetMenu(menu)

	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(_ *application.ApplicationEvent) {
		go service.AutoStartConfigured()
	})
	app.OnShutdown(func() {
		manager.Shutdown()
	})

	if err := app.Run(); err != nil {
		log.Printf("application stopped: %v", err)
	}
}

func defaultSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.ssh/config"
	}
	return filepath.Join(home, ".ssh", "config")
}

func defaultStorePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "ssh-port-forward", "forwards.json"), nil
}

package app

import (
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"

	"zakki/locales"
)

// updateOptions 让更新窗口跟随应用当前语言：Language 留空时插件在每次开窗时取
// mygo.App.Locale()，切换界面语言后下一个窗口立即跟着变。写死一种语言会让另一种
// 界面的用户看到不同语言的发布说明与按钮。
//
// 只开手动检查：失败重试与发布说明渲染都在插件内部（重试只影响自动检查的调度），
// 安装完成后的重启走退出钩子 finishQuit，保存失败会取消重启，无需 OnInstalled。
func updateOptions() updater.Options {
	return updater.Options{
		DisableAutomaticChecks: true,
	}
}

func updatePlugin() mygo.Plugin {
	return native.New(updateOptions())
}

func updateMenuItem() *mygo.MenuItem {
	// 不依赖插件初始化来组装菜单，视图测试无需启动更新器或访问真实用户目录。
	return &mygo.MenuItem{
		ID:    "check-updates",
		Label: locales.MenuCheckUpdates(),
		Click: func(*mygo.MenuItem, *mygo.Window) { updater.CheckForUpdates() },
	}
}

func (a *app) finishQuit(event *mygo.QuitEvent) {
	var err error
	if !a.onboarding.finished && (a.onboarding.obWin != nil || a.onboarding.exitWithoutSetup) {
		err = a.shutdownOnboardingExit()
	} else {
		err = a.shutdownForQuit()
	}
	if err != nil {
		// 更新器的重启也经过退出钩子；保存失败时保留运行中的编辑会话。
		event.PreventDefault()
		a.onboarding.exitWithoutSetup = false
		a.showToast(locales.SaveFailedQuitCancelled(err.Error()))
		slog.Error("保存失败，已取消退出或更新重启", "error", err)
		if a.ready {
			a.showWindow()
		}
	}
}

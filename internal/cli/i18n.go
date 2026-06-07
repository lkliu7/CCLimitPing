package cli

import (
	"os"
	"strings"
)

type cliText struct {
	rootShort     string
	rootLong      string
	helpFlag      string
	usageTemplate string

	helpCommandShort string
	helpCommandLong  string
	helpUnknownTopic string

	completionShort      string
	completionLong       string
	completionNoDescFlag string
	completionShellShort string
	completionShellLong  string

	versionShort string

	statusShort       string
	statusLong        string
	statusVerboseFlag string

	pingShort      string
	pingLong       string
	pingDryRunFlag string

	watchShort      string
	watchLong       string
	watchDryRunFlag string

	configShort     string
	configInitShort string
	configInitForce string
	configPathShort string

	upgradeShort string
	upgradeLong  string

	uninstallShort      string
	uninstallLong       string
	uninstallKeepConfig string
}

func localizedText() cliText {
	if isChineseLocale() {
		return zhText
	}
	return enText
}

func isChineseLocale() bool {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANGUAGE", "LANG"} {
		locale := strings.ToLower(os.Getenv(key))
		if locale == "" {
			continue
		}
		for _, part := range strings.FieldsFunc(locale, func(r rune) bool {
			return r == ':' || r == '.' || r == '@' || r == '_' || r == '-'
		}) {
			if part == "zh" || strings.HasPrefix(part, "zh") {
				return true
			}
		}
	}
	return false
}

var enText = cliText{
	rootShort: "Keep Claude Code / Codex rate-limit windows back-to-back",
	rootLong:  "limitping pings your AI coding provider the moment its 5h rate-limit window resets, so the next window starts immediately and stays aligned. Usage is read via zero-quota endpoints; pings go through the official CLIs.",
	helpFlag:  "help for this command",
	usageTemplate: `Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

Additional Commands:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`,

	helpCommandShort: "Help about any command",
	helpCommandLong:  "Help provides help for any command in the application.\nType limitping help [command] for full details.",
	helpUnknownTopic: "Unknown help topic",

	completionShort:      "Generate shell completion scripts",
	completionLong:       "Generate shell completion scripts for limitping.\n\nRun `limitping completion [bash|zsh|fish|powershell] --help` for shell-specific usage.",
	completionNoDescFlag: "disable completion descriptions",
	completionShellShort: "Generate the %s completion script",
	completionShellLong:  "Generate the %s completion script for limitping.",

	versionShort: "Print the version",

	statusShort:       "Show current 5h/weekly usage and reset countdowns without using quota",
	statusLong:        "Show current 5h and weekly usage for every enabled provider. This command only reads usage data from zero-quota endpoints; it does not send a ping or consume model quota.",
	statusVerboseFlag: "print the raw JSON response",

	pingShort: "Trigger a provider window now with a minimal message",
	pingLong: `Trigger a rate-limit window immediately by sending the minimal message for the selected provider.

Arguments:
  provider  Optional. One of: claude, codex, glm, all.
            Defaults to all, which pings every enabled provider.

Examples:
  limitping ping
  limitping p claude
  limitping ping codex --dry-run`,
	pingDryRunFlag: "print the command without sending",

	watchShort: "Run the foreground daemon and ping each provider when its 5h window resets",
	watchLong: `Run the foreground daemon. When a provider's 5h window resets, limitping sends the minimal message to start the next window.

Arguments:
  provider  Optional. One of: claude, codex, glm, all.
            Defaults to all, which watches every enabled provider.

Examples:
  limitping watch
  limitping w claude
  limitping watch --dry-run`,
	watchDryRunFlag: "log when pings would fire without sending them",

	configShort:     "Manage the configuration file",
	configInitShort: "Write a default config file",
	configInitForce: "overwrite an existing config",
	configPathShort: "Print the config file path",

	upgradeShort: "Upgrade limitping to the latest release",
	upgradeLong:  "Download the latest GitHub release for this OS/architecture and replace the currently running limitping binary.",

	uninstallShort:      "Remove limitping and its config/cache",
	uninstallLong:       "Remove the currently running limitping binary and its config/cache directory. Pass --keep-config to preserve config/cache files.",
	uninstallKeepConfig: "preserve the limitping config/cache directory",
}

var zhText = cliText{
	rootShort: "让 Claude Code / Codex 的限额窗口自动接龙",
	rootLong:  "limitping 会在 AI 编程 Provider 的 5h 限额窗口重置时立即发送 ping，让下一个窗口马上开始并保持对齐。用量读取走零消耗接口；ping 通过官方 CLI 发送。",
	helpFlag:  "显示此命令的帮助",
	usageTemplate: `用法:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

别名:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

示例:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

可用命令:{{range $cmds}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range $cmds}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{end}}{{if not .AllChildCommandsHaveGroup}}

其他命令:{{range $cmds}}{{if (and (eq .GroupID "") (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .NameAndAliases 24}} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

选项:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

全局选项:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

其他帮助主题:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

使用 "{{.CommandPath}} [command] --help" 查看命令详情。{{end}}
`,

	helpCommandShort: "查看任意命令的帮助",
	helpCommandLong:  "查看应用中任意命令的帮助。\n输入 limitping help [command] 查看完整详情。",
	helpUnknownTopic: "未知帮助主题",

	completionShort:      "生成 shell 补全脚本",
	completionLong:       "生成 limitping 的 shell 补全脚本。\n\n运行 `limitping completion [bash|zsh|fish|powershell] --help` 查看指定 shell 的用法。",
	completionNoDescFlag: "禁用补全说明",
	completionShellShort: "生成 %s 补全脚本",
	completionShellLong:  "生成 limitping 的 %s 补全脚本。",

	versionShort: "打印版本号",

	statusShort:       "查看当前 5h/周用量和重置倒计时，不消耗额度",
	statusLong:        "查看所有已启用 Provider 的当前 5h 和周用量。此命令只通过零消耗接口读取用量，不会发送 ping，也不会消耗模型额度。",
	statusVerboseFlag: "打印原始 JSON 响应",

	pingShort: "用最小消息立即触发 Provider 的限额窗口",
	pingLong: `通过向指定 Provider 发送最小消息，立即触发一个限额窗口。

参数:
  provider  可选。取值: claude、codex、glm、all。
            默认是 all，会 ping 所有已启用的 Provider。

示例:
  limitping ping
  limitping p claude
  limitping ping codex --dry-run`,
	pingDryRunFlag: "只打印将执行的命令，不真正发送",

	watchShort: "以前台守护方式运行，并在每个 Provider 的 5h 窗口重置时自动 ping",
	watchLong: `以前台守护方式运行。某个 Provider 的 5h 窗口重置后，limitping 会发送最小消息来开启下一个窗口。

参数:
  provider  可选。取值: claude、codex、glm、all。
            默认是 all，会监测所有已启用的 Provider。

示例:
  limitping watch
  limitping w claude
  limitping watch --dry-run`,
	watchDryRunFlag: "只记录何时会触发，不真正发送",

	configShort:     "管理配置文件",
	configInitShort: "写入默认配置文件",
	configInitForce: "覆盖已有配置",
	configPathShort: "打印配置文件路径",

	upgradeShort: "将 limitping 更新到最新版本",
	upgradeLong:  "下载适用于当前系统和架构的最新 GitHub Release，并替换正在运行的 limitping 二进制文件。",

	uninstallShort:      "删除 limitping 及其配置/缓存",
	uninstallLong:       "删除当前运行的 limitping 二进制文件及配置/缓存目录。使用 --keep-config 可保留配置/缓存文件。",
	uninstallKeepConfig: "保留 limitping 配置/缓存目录",
}

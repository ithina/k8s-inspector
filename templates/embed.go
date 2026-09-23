// Package templates 以 embed 方式内置项目使用的静态资源，
// 使二进制无需依赖外部模板文件即可生成 HTML 报告。
package templates

import "embed"

// FS 内置模板文件系统，包含 report.html。
//
//go:embed report.html
var FS embed.FS

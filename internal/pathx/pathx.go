// Package pathx 处理 MSYS(POSIX)风格路径与 Windows 原生路径的互转。
//
// 背景:同一份测试/配置里既有 "C:/Users/..." 也有 bash 传下来的 "/tmp/..."。
// 原生进程(本程序)不认识 "/tmp",需要显式映射,否则 stat/git 都会失败。
package pathx

import (
	"os"
	"strings"
)

// Native 把常见的 POSIX 路径映射成 Windows 原生路径;已是原生形式则原样返回。
func Native(p string) string {
	if p == "" || !strings.HasPrefix(p, "/") {
		return p
	}
	// /c/Users/... -> C:/Users/...
	if len(p) >= 3 && p[2] == '/' {
		c := p[1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return strings.ToUpper(string(c)) + ":" + p[2:]
		}
	}
	// /tmp/... -> %TEMP%/...
	if p == "/tmp" {
		return os.TempDir()
	}
	if strings.HasPrefix(p, "/tmp/") {
		return os.TempDir() + p[len("/tmp"):]
	}
	return p
}

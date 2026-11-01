package web

import (
	"embed"
	"io/fs"
	"log"
)

// 前端资源通过 go:embed 打进二进制。
//
// ⚠️ 关键约束：embed 的模式匹配不到任何文件时是**编译错误**，
// 而 web/dist 是构建产物（干净 clone 后不存在）。
// 因此这里只 embed 一个**必然存在**的占位目录，
// 真实前端产物由 `web/dist` 在构建时覆盖 —— 见下面的 Assets()。
//
// 换句话说：
//   - 有 web/dist 的构建（make build-web 之后）：embed 到的是 dist 内容
//   - 没有 web/dist 的构建：embed 到的是占位页，后端 API 仍完全可用
//
// 实现方式：构建前把目标目录内容复制到 embed_dir，再编译。
// 详见 Makefile 的 build-web / prepare-embed 目标。

//go:embed embed_dir
var distFS embed.FS

// Assets 返回前端静态资源文件系统。
func Assets() fs.FS {
	sub, err := fs.Sub(distFS, "embed_dir")
	if err != nil {
		log.Printf("无法加载前端资源: %v", err)
		return distFS
	}
	return sub
}

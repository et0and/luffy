// Package assets embeds the default web content.
//
// The content comes from the original PirateBox project, with the brand
// names replaced. Luffy can write it to the USB stick on
// `luffy setup content`, so a router only needs the single binary.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed all:www_content
var content embed.FS

// WWWContent returns the embedded default web content rooted at
// www_content.
func WWWContent() fs.FS {
	sub, err := fs.Sub(content, "www_content")
	if err != nil {
		// The embedded FS is built at compile time; this cannot fail.
		panic(err)
	}
	return sub
}

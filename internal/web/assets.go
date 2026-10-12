package web

import "embed"

// Assets includes the editor bundle so deployed browsers need no external CDN.
//
//go:embed assets/*
var Assets embed.FS

var IndexHTML, _ = Assets.ReadFile("assets/index.html")

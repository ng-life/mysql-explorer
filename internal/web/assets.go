package web

import _ "embed"

// IndexHTML is embedded so releases need no separate static directory.
//
//go:embed assets/index.html
var IndexHTML []byte

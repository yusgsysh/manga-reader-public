// Package assets holds the application icon. The PNG is generated once by
// gen.go (go generate) and committed, so builds need no drawing code and the
// file can also be installed as the desktop icon.
package assets

import _ "embed"

//go:generate go run gen.go

// PNG is the 512×512 application icon.
//
//go:embed icon.png
var PNG []byte

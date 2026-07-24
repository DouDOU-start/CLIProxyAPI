// Package managementweb exposes the management console built from the colocated frontend source.
package managementweb

import _ "embed"

//go:embed dist/index.html
var indexHTML []byte

// IndexHTML returns a copy of the embedded management console document.
func IndexHTML() []byte {
	return append([]byte(nil), indexHTML...)
}

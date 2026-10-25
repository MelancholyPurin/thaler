// Package thaler holds the files that ship with the source and are needed at
// run time. The program itself is in cmd/thaler.
package thaler

import _ "embed"

// ConfigExample is config.example.toml, the template for a new config file.
//
//go:embed config.example.toml
var ConfigExample string

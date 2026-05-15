// Package mws is the module root. It exposes the embedded skeleton content.
package mws

import "embed"

// SkeletonFS holds the templated meta-workspace skeleton bundled into the binary.
// The contents are rendered out to disk by `mws init` and `mws migrate`.
//
//go:embed all:skeleton
var SkeletonFS embed.FS

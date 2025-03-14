//go:build tools

package gno

// this file is used to allow "go mod" to be aware of some development dependencies.

import (
	// required by Makefile for flappy tests
	_ "moul.io/testman"

	// required to generate String method
	_ "golang.org/x/tools/cmd/stringer"

	// required for import formatting
	_ "golang.org/x/tools/cmd/goimports"

	// required for formatting, linting, pls.
	_ "mvdan.cc/gofumpt"

	// protoc, genproto
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"

	// gen docs
	_ "golang.org/x/tools/cmd/godoc"

	// linter
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"

	// embedmd
	_ "github.com/campoy/embedmd/embedmd"
)

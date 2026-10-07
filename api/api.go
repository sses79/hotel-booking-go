// Package api embeds documentation so the deployed binary needs no filesystem.
package api

import "embed"

//go:embed openapi.yaml assets/*
var Files embed.FS

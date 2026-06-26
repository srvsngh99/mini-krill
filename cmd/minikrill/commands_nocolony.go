//go:build !colony

package main

import "context"

// maybeStartFeed is a no-op in the public Mini-Krill release: the Reef social
// feed and the Chroma-backed social memory are colony-only features, compiled in
// only under `-tags colony` (see commands_colony.go). This keeps the shipped
// binary lean — no feed, no Chroma dependency.
func maybeStartFeed(_ context.Context, _ *krillStack) {}

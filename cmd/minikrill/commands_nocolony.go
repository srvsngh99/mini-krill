//go:build !colony

package main

import "context"

// maybeStartColony is a no-op in the public Mini-Krill release: the Reef social
// feed and the vector-backed semantic (social) memory are colony-only features,
// compiled in only under `-tags colony` (see commands_colony.go). Because the
// agent's semantic memory is only ever set from there, the public agent keeps a
// nil store and behaves exactly as before - no feed, no vector-store dependency.
func maybeStartColony(_ context.Context, _ *krillStack) {}

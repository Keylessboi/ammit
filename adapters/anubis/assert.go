package anubis

import (
	"github.com/TecharoHQ/anubis/lib/challenge/extension"
)

// Compile-time proof that this package satisfies the interface Anubis's
// registry expects.
//
// Registration goes through a map lookup at init time, so a method whose
// signature drifted would not fail to compile anywhere else: the package would
// simply never be selected, and Ammit would be silently absent from a running
// Anubis. This assertion turns that class of failure into a build error.
var _ extension.Impl = (*Extension)(nil)

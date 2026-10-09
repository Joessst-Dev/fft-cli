package skill

// Stamp and Unstamp expose the frontmatter edit so its exactness can be pinned
// directly. Through an install the only stamp a spec can produce is the one the
// running binary writes, and the cases that matter — a user's own metadata block,
// a version line that is nearly fft's — are ones no install would write.
var (
	Stamp   = stamp
	Unstamp = unstamp
)

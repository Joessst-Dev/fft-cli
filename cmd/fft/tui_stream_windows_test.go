//go:build windows

package main

import . "github.com/onsi/ginkgo/v2"

// breakComponentRoot cannot force the failure this spec needs on Windows, so the
// spec is skipped here rather than weakened into one that would pass.
//
// What it needs is a root that exists and cannot be listed. Replacing the root
// with a regular file — what the other platforms do, and what fails with ENOTDIR
// there regardless of privilege — does not do it: os.ReadDir on a path that is
// not a directory reports the same shape Windows reports for a path that was
// never there, which component.Open deliberately treats as "no components", not
// as a failure. Denying the account's own SID the right to list the directory
// does not do it either: CI runs elevated, and the deny did not hold — its own
// self-check said so.
//
// The logic being pinned is not platform-specific. rescanComponents discards a
// scan carrying a Problem on the root, and the other platforms' legs check that
// on every run; only the lever for producing such a scan is missing here.
func breakComponentRoot(_ string) {
	GinkgoHelper()
	Skip("no reliable way on Windows to make a directory exist but fail to list: " +
		"replacing it with a file reports as missing, and a deny ACE does not hold for an elevated CI account")
}

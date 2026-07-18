package emulator

// The suite is in package emulator, not emulator_test: it asserts on the store, the
// pagination and the collection inference directly, all of which are unexported —
// the exported surface is a running HTTP server, and testing only that would make a
// wrong cursor look like a wrong handler.

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEmulator(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/emulator")
}

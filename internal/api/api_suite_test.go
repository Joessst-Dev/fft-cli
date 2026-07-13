package api

// The suite is in package api, not api_test: readPOSTs is unexported on purpose —
// nothing outside this package should key off it — and guarding that table is
// most of what this suite does.

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/api")
}

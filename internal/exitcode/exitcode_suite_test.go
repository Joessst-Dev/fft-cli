package exitcode_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestExitcode(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/exitcode")
}

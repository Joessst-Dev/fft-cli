package docsmd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDocsmd(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/docsmd")
}

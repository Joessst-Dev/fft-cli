package shellquote_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestShellquote(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "shellquote")
}

package prompt_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPrompt(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "internal/prompt")
}

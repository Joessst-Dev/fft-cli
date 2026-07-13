package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSpecgen(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "tools/specgen")
}

// find returns the loaded operation with that id, failing the spec if there is none.
func find(ops []operation, id string) operation {
	GinkgoHelper()

	for _, op := range ops {
		if op.ID == id {
			return op
		}
	}
	Fail("the mini spec has no operation " + id)
	return operation{}
}

// paramOf returns an operation's parameter by name and location.
func paramOf(op operation, in, name string) param {
	GinkgoHelper()

	for _, p := range op.Params {
		if p.In == in && p.Name == name {
			return p
		}
	}
	Fail("operation " + op.ID + " has no " + in + " parameter " + name)
	return param{}
}

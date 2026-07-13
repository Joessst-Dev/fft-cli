package secrets_test

import (
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/zalando/go-keyring"

	"github.com/Joessst-Dev/fft-cli/internal/secrets"
	"github.com/Joessst-Dev/fft-cli/internal/testsupport"
)

var _ = Describe("Key", func() {
	It("namespaces a secret by project and kind", func() {
		Expect(secrets.Key("staging", secrets.KindRefreshToken)).To(Equal("fft:staging:refreshToken"))
	})

	It("round-trips through ParseKey", func() {
		project, kind, ok := secrets.ParseKey(secrets.Key("staging", secrets.KindIDToken))

		Expect(ok).To(BeTrue())
		Expect(project).To(Equal("staging"))
		Expect(kind).To(Equal(secrets.KindIDToken))
	})

	DescribeTable("rejecting a key it did not produce",
		func(key string) {
			_, _, ok := secrets.ParseKey(key)

			Expect(ok).To(BeFalse())
		},
		Entry("without the fft prefix", "staging:password"),
		Entry("without a kind", "fft:staging"),
		Entry("with an empty project", "fft::password"),
		Entry("empty", ""),
	)
})

// storeContract is the behaviour every implementation owes its callers. Running
// it against each of them is what stops the keychain and the file store from
// quietly disagreeing about what a missing secret looks like.
func storeContract(name string, newStore func() secrets.Store) {
	Describe(name, func() {
		var store secrets.Store

		BeforeEach(func() {
			store = newStore()
		})

		It("returns what it was given", func() {
			Expect(store.Set("fft:staging:password", "s3cret")).To(Succeed())

			Expect(store.Get("fft:staging:password")).To(Equal("s3cret"))
		})

		It("reports a key it holds nothing for as ErrNotFound", func() {
			_, err := store.Get("fft:staging:password")

			Expect(err).To(MatchError(secrets.ErrNotFound))
		})

		It("replaces a secret rather than appending to it", func() {
			Expect(store.Set("fft:staging:password", "old")).To(Succeed())
			Expect(store.Set("fft:staging:password", "new")).To(Succeed())

			Expect(store.Get("fft:staging:password")).To(Equal("new"))
		})

		It("forgets a deleted secret", func() {
			Expect(store.Set("fft:staging:password", "s3cret")).To(Succeed())
			Expect(store.Delete("fft:staging:password")).To(Succeed())

			_, err := store.Get("fft:staging:password")
			Expect(err).To(MatchError(secrets.ErrNotFound))
		})

		It("treats deleting a secret it never had as a no-op, not a failure", func() {
			Expect(store.Delete("fft:staging:password")).To(Succeed())
		})

		It("keeps one project's secrets separate from another's", func() {
			Expect(store.Set("fft:staging:password", "staging-secret")).To(Succeed())
			Expect(store.Set("fft:prod:password", "prod-secret")).To(Succeed())

			Expect(store.Get("fft:staging:password")).To(Equal("staging-secret"))
			Expect(store.Get("fft:prod:password")).To(Equal("prod-secret"))
		})
	})
}

var _ = Describe("the store implementations", func() {
	storeContract("MemStore", func() secrets.Store {
		return secrets.NewMem()
	})

	storeContract("the keychain store", func() secrets.Store {
		// zalando ships a mock keychain, which is the only way to exercise this on
		// a machine (or a CI runner) with no Secret Service.
		keyring.MockInit()
		return secrets.NewKeyring()
	})

	storeContract("the file store", func() secrets.Store {
		return secrets.NewFile(filepath.Join(GinkgoT().TempDir(), "fft", "credentials.json"))
	})
})

var _ = Describe("the file store", func() {
	var path string

	BeforeEach(func() {
		path = filepath.Join(GinkgoT().TempDir(), "fft", "credentials.json")
	})

	It("writes the credentials file with mode 0600 and its directory with 0700", func() {
		Expect(secrets.NewFile(path).Set("fft:staging:password", "s3cret")).To(Succeed())

		testsupport.ExpectOwnerOnlyFile(path)
		testsupport.ExpectOwnerOnlyDir(filepath.Dir(path))
	})

	It("survives a new process reading what an old one wrote", func() {
		Expect(secrets.NewFile(path).Set("fft:staging:password", "s3cret")).To(Succeed())

		Expect(secrets.NewFile(path).Get("fft:staging:password")).To(Equal("s3cret"))
	})

	It("names itself 'file', which is what the CREDENTIAL column shows", func() {
		Expect(secrets.NewFile(path).Kind()).To(Equal("file"))
	})
})

var _ = Describe("the environment store", func() {
	var env map[string]string

	store := func() secrets.Store {
		return secrets.NewEnv(func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		})
	}

	BeforeEach(func() {
		env = map[string]string{
			"FFT_PASSWORD": "s3cret",
			"FFT_ID_TOKEN": "eyJhbGciOi...",
		}
	})

	It("reads the password from FFT_PASSWORD", func() {
		Expect(store().Get(secrets.Key("env", secrets.KindPassword))).To(Equal("s3cret"))
	})

	It("reads the id token from FFT_ID_TOKEN", func() {
		Expect(store().Get(secrets.Key("env", secrets.KindIDToken))).To(Equal("eyJhbGciOi..."))
	})

	It("ignores the project part of the key, because a CI job has exactly one project", func() {
		Expect(store().Get(secrets.Key("anything-at-all", secrets.KindPassword))).To(Equal("s3cret"))
	})

	It("reports an unset variable as ErrNotFound", func() {
		_, err := store().Get(secrets.Key("env", secrets.KindRefreshToken))

		Expect(err).To(MatchError(secrets.ErrNotFound))
	})

	It("refuses to be written to, because a CI runner has nowhere durable to put a token", func() {
		Expect(store().Set(secrets.Key("env", secrets.KindIDToken), "new")).To(MatchError(secrets.ErrReadOnly))
	})

	It("names itself 'env', which is what the CREDENTIAL column shows", func() {
		Expect(store().Kind()).To(Equal("env"))
	})
})

var _ = Describe("storing a project's secrets", func() {
	var store *secrets.MemStore

	BeforeEach(func() {
		store = secrets.NewMem()
	})

	It("gives every secret its own key, never one bundled blob", func() {
		// The Windows Credential Manager caps one credential at roughly 2.5 KB and
		// a Firebase id token alone is about 1 KB, so a blob holding all four would
		// silently fail to save there.
		for _, kind := range secrets.AllKinds() {
			Expect(store.Set(secrets.Key("staging", kind), "value-of-"+kind)).To(Succeed())
		}

		Expect(store.Snapshot()).To(HaveLen(4))
		Expect(store.Snapshot()).To(HaveKey("fft:staging:password"))
		Expect(store.Snapshot()).To(HaveKey("fft:staging:refreshToken"))
		Expect(store.Snapshot()).To(HaveKey("fft:staging:idToken"))
		Expect(store.Snapshot()).To(HaveKey("fft:staging:idTokenExp"))
	})

	Describe("DeleteAll", func() {
		BeforeEach(func() {
			for _, kind := range secrets.AllKinds() {
				Expect(store.Set(secrets.Key("staging", kind), "value")).To(Succeed())
			}
			Expect(store.Set(secrets.Key("prod", secrets.KindPassword), "other")).To(Succeed())
		})

		It("removes every one of the project's keys, leaving nothing in the keychain", func() {
			Expect(secrets.DeleteAll(store, "staging")).To(Succeed())

			Expect(store.Snapshot()).To(HaveLen(1))
			Expect(store.Snapshot()).To(HaveKey("fft:prod:password"))
		})

		It("leaves another project's secrets alone", func() {
			Expect(secrets.DeleteAll(store, "staging")).To(Succeed())

			Expect(store.Get("fft:prod:password")).To(Equal("other"))
		})

		It("succeeds on a project that has no secrets at all", func() {
			Expect(secrets.DeleteAll(store, "never-configured")).To(Succeed())
		})
	})

	Describe("Has", func() {
		It("is true when a password is stored", func() {
			Expect(store.Set(secrets.Key("staging", secrets.KindPassword), "s3cret")).To(Succeed())

			Expect(secrets.Has(store, "staging")).To(BeTrue())
		})

		It("is true when only an id token is stored, which is credential enough", func() {
			Expect(store.Set(secrets.Key("staging", secrets.KindIDToken), "eyJ...")).To(Succeed())

			Expect(secrets.Has(store, "staging")).To(BeTrue())
		})

		It("is false when only a refresh token's expiry is stored", func() {
			Expect(store.Set(secrets.Key("staging", secrets.KindIDTokenExp), "123")).To(Succeed())

			Expect(secrets.Has(store, "staging")).To(BeFalse())
		})

		It("is false for a project with nothing stored", func() {
			Expect(secrets.Has(store, "staging")).To(BeFalse())
		})
	})
})

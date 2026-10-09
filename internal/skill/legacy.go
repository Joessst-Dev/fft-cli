package skill

// legacy is every file of the skill as fft shipped it before installs carried a
// manifest, keyed by its path within the skill: the sha256 of the bytes, as
// [digest] spells it.
//
// It is what lets an install made by one of those releases upgrade without being
// asked about. Those installs recorded nothing, so a file in one that matches
// none of today's bytes could be fft's own old text or the user's edit, and the
// only way to tell is to know every text fft ever shipped. A file that matches
// one of these is fft's, untouched; anything else is still the user's.
//
// Frozen. Every install from now on writes a manifest ([ManifestName]), so nothing
// is ever added here. It was produced once, by hashing
//
//	git show <ref>:internal/skill/assets/<file>
//
// for every file at every v* tag that has the directory (v0.2.0 to v0.8.0; v0.1.0
// and v0.1.0-rc1 predate the skill) and at main as of 8530c3f, the last commit
// before stamping. That commit was never tagged, but every dev build and every
// `go install …@main` from it wrote these bytes, and they deserve the same clean
// upgrade. The comment on each hash names where it shipped.
var legacy = map[string][]string{
	"SKILL.md": {
		"sha256:f7e61af2248ab05feae89fe6462fe6eb4b0d242e6fcdce9faf19053c51928903", // v0.2.0
		"sha256:497f83f548090374b6b35d80bb7f0d1f4ad3296b5faf1f87676dec01c6130dcf", // v0.3.0, v0.4.0
		"sha256:1d404712be163aba5acb16de2af39044610e61e81895be9cfd031e45eb87065a", // v0.5.0
		"sha256:4982e81a6fe59d052deb45551989a07c935dddc0199c010398de6834286594e8", // v0.6.0, v0.7.0
		"sha256:b31ea488b5fbc52bfa969688fa923195525cd3a1df987df8bc2710441f151559", // v0.8.0
		"sha256:3d656071b6ec347f48723efa156a1e73110cbb908e2824b0db900d70e1ed2f45", // main
	},
	"references/commands.md": {
		"sha256:247aa9beee7797c60c2e66295487a231e7fde31d2815c4f42f40a71512517546", // v0.2.0
		"sha256:9bf9b96762a35bb3e33409635671dd87fc1b754ed785d289a6c419bb6eb015b7", // v0.3.0, v0.4.0, v0.5.0
		"sha256:50bfb9a0fa59f54a7f4a7e02b50e618864739695dca1836ecaf0798e6bc0f5b2", // v0.6.0, v0.7.0
		"sha256:7d71add02049c3c1bdf7de62f44369c84090a1fc04ca7e799a1b69af0d590572", // v0.8.0, main
	},
	"references/components.md": {
		"sha256:19b87e229716f9e0b8c2c321eb3f66f999ae3d8821fb22176675cf6b2f20697e", // v0.6.0, v0.7.0
		"sha256:283edc87e954aa9e166f39c47204f7215c52276602e8046cafe997548a635869", // v0.8.0, main
	},
	"references/discovery.md": {
		"sha256:a9d52ff6c58afa03003f4ae86175ed7c68567620a9e31898aea6bae273465543", // v0.2.0, v0.3.0, v0.4.0, v0.5.0, v0.6.0, v0.7.0
		"sha256:f8571bfdff3a7b66a5a4a22d9f6510b892b2b51b3e1ad4b6ce81e8908c00252b", // v0.8.0
		"sha256:47192649bacf15567f5da0f00f462705440a45e616673223f0609a85bbfb29ae", // main
	},
	"references/emulator.md": {
		"sha256:2c4912d6ecf5dfd7e535f296e66b32dbc66d0bef5c118034997f345733b9ec0f", // v0.3.0
		"sha256:9942437d36e2a983a1025b74b91d646db5287cf16faefc9ae4209cb7f236d0d4", // v0.4.0
		"sha256:2d771aecc90be1bb7a9abfd45f0a7063b6b1057cc096b965acd63b159b5d115a", // v0.5.0
		"sha256:2a01078fe5949193da2768e31421d5a67474b59fd84c1a91acd3f260bdae0173", // v0.6.0, v0.7.0
		"sha256:2aa443ac775d21dcc0e7dd01577d798970433998ffbb93de0b2af445dd3cde33", // v0.8.0, main
	},
	"references/recipes.md": {
		"sha256:be4f2a985c9a38e6d84a7d9e991611ae405205f3a2380be73def6fd4567e8495", // v0.2.0, v0.3.0
		"sha256:a0e2b931ed0c63c3f8dbf5a4c97ebf1c7e67ea59df75e52a009ffdbeb3a082ce", // v0.4.0, v0.5.0, v0.6.0, v0.7.0
		"sha256:129bc3252ff9b31a09ac3f30f4c70fcb47703a698c65a8f168030e5c3d382cc7", // v0.8.0, main
	},
	"references/templates.md": {
		"sha256:a3e334cb5853dca5257ea463b961e2321e89a1a55076bbcdfc92156556f8ecfd", // v0.8.0, main
	},
	"references/troubleshooting.md": {
		"sha256:6b05efaf65b3128e8568f749dd8f33873c92748d378d1d92c4d0b8c273f36a93", // v0.2.0, v0.3.0, v0.4.0, v0.5.0, v0.6.0
		"sha256:5eadf66948a00049b4465fc1723acb40340b5f7702e89474dc8f29a25ab7dea2", // v0.7.0, v0.8.0, main
	},
}

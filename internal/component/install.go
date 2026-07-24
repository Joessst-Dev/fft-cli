package component

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultRepo is where a first-party component comes from. A short name —
// `fft component install emulator` — resolves here.
const DefaultRepo = "Joessst-Dev/fft-cli"

// githubAPI is GitHub's release endpoint, formatted with the repo and the tag.
const githubAPI = "https://api.github.com"

// Limits on what will be downloaded and unpacked. They are generous enough for a
// component carrying a static Go binary and small enough that a hostile or
// corrupted archive cannot fill the disk before anyone notices.
const (
	maxArchive  = 256 << 20 // 256 MiB
	maxFile     = 256 << 20
	maxFiles    = 2048
	maxChecksum = 1 << 20
	maxRelease  = 4 << 20
)

// File modes inside an installed component. The executable bit is carried over
// from the archive; everything else is normalised, because a tarball's modes are
// whoever built it's business and not something to reproduce faithfully.
const (
	fileMode = 0o644
	execMode = 0o755
)

// Source is where a component is being installed from.
type Source struct {
	// Repo is an owner/repo on GitHub.
	Repo string

	// Version is the release tag, or "" for the latest release.
	Version string

	// Name is the component's expected name, when the spec implied one. It is
	// checked against the manifest in the archive: an install of `emulator` that
	// unpacks something called otherwise is a mismatch worth stopping on.
	Name string

	// Dir is a local directory to install from, which is how a component is
	// developed and how a release archive is installed after being unpacked by hand.
	// It is exclusive with Repo.
	Dir string
}

// String renders the source the way `fft component list` records it.
func (s Source) String() string {
	switch {
	case s.Dir != "":
		return s.Dir
	case s.Version != "":
		return "github.com/" + s.Repo + "@" + s.Version
	default:
		return "github.com/" + s.Repo
	}
}

// ParseSource reads an install spec.
//
// Three forms, in the order a user is likely to type them:
//
//	emulator              a component fft ships, from its own releases
//	owner/repo            somebody else's, latest release
//	owner/repo@v1.2.3     somebody else's, pinned
func ParseSource(spec string) (Source, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Source{}, errors.New("no component named")
	}

	// A URL is a reasonable thing to paste, and refusing it with the form that does
	// work is better than trying to guess which of its paths is the repository.
	if strings.Contains(spec, "://") {
		return Source{}, fmt.Errorf("%q is a URL: name the repository as owner/repo instead", spec)
	}

	repo, version, _ := strings.Cut(spec, "@")
	repo = strings.TrimSuffix(repo, "/")

	switch strings.Count(repo, "/") {
	case 0:
		if !nameRE.MatchString(repo) {
			return Source{}, fmt.Errorf("%q is neither a component name nor an owner/repo", spec)
		}
		return Source{Repo: DefaultRepo, Version: version, Name: repo}, nil
	case 1:
		owner, name, _ := strings.Cut(repo, "/")
		if owner == "" || name == "" {
			return Source{}, fmt.Errorf("%q is not an owner/repo", spec)
		}
		return Source{Repo: repo, Version: version}, nil
	default:
		return Source{}, fmt.Errorf("%q is not an owner/repo", spec)
	}
}

// Installer downloads, verifies and unpacks components.
//
// It reaches GitHub and nothing else, with its own http.Client: the API client
// carries a bearer token for the tenant, and a release download has no business
// anywhere near it.
type Installer struct {
	root   string
	api    string
	client *http.Client
}

// InstallerOption configures an [Installer].
type InstallerOption func(*Installer)

// WithAPI replaces GitHub's endpoint. Specs point it at an httptest server.
func WithAPI(url string) InstallerOption {
	return func(i *Installer) { i.api = strings.TrimRight(url, "/") }
}

// WithHTTPClient replaces the client the installer downloads with.
func WithHTTPClient(c *http.Client) InstallerOption {
	return func(i *Installer) { i.client = c }
}

// NewInstaller returns an Installer writing into root.
func NewInstaller(root string, opts ...InstallerOption) *Installer {
	i := &Installer{
		root: root,
		api:  githubAPI,

		// No deadline of its own: the caller's context bounds the download, and a
		// component archive on a slow connection is a legitimately long request that a
		// baked-in timeout would cut off for no reason.
		client: &http.Client{},
	}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Plan is what an install would do, worked out before anything is written.
//
// It exists so the user can be shown the source, the version and the fact that a
// component runs as they do, and can say no — the confirmation is not a formality,
// it is the only point at which a human decides to trust this code.
type Plan struct {
	// Source is where it is coming from.
	Source Source

	// Manifest is what the archive says it is. It is read from the staged, verified
	// archive, so by the time a Plan exists the download has happened and been
	// checked; what has *not* happened is anything to the installed tree.
	Manifest Manifest

	// Dir is where it will be installed.
	Dir string

	// Replaces is the version already installed there, or "" for a fresh install.
	Replaces string

	// Digest is the SHA-256 of the archive, as verified. Empty for a --path install,
	// which has no archive and no checksums file to check it against.
	Digest string

	// Signed reports that the release publishes a cosign signature for this archive.
	// fft does not verify it — see [Plan.Verification].
	Signed bool

	// staged is the unpacked tree, waiting to be renamed into place.
	staged string
}

// Verification is what the user should be told about the trust in this install,
// in one line.
func (p Plan) Verification() string {
	switch {
	case p.Digest == "":
		return "unverified (installed from a local directory)"
	case p.Signed:
		return fmt.Sprintf("sha256:%s, and the release is cosign-signed (verify it with: cosign verify-blob)", short(p.Digest))
	default:
		return fmt.Sprintf("sha256:%s", short(p.Digest))
	}
}

func short(digest string) string {
	if len(digest) <= 16 {
		return digest
	}
	return digest[:16]
}

// Prepare downloads and verifies a component, and stages it, without touching the
// installed tree. Call [Installer.Commit] to finish, or [Installer.Discard] to
// abandon it.
func (i *Installer) Prepare(ctx context.Context, src Source) (Plan, error) {
	if i.root == "" {
		return Plan{}, errors.New("components are disabled: " + EnvRoot + " is set to the empty string")
	}
	if err := os.MkdirAll(i.root, dirMode); err != nil {
		return Plan{}, fmt.Errorf("create %s: %w", i.root, err)
	}

	staged, err := os.MkdirTemp(i.root, ".staging-")
	if err != nil {
		return Plan{}, fmt.Errorf("create a staging directory in %s: %w", i.root, err)
	}

	plan, err := i.stage(ctx, src, staged)
	if err != nil {
		_ = os.RemoveAll(staged)
		return Plan{}, err
	}
	return plan, nil
}

// stage fills the staging directory and reads back what landed in it.
func (i *Installer) stage(ctx context.Context, src Source, staged string) (Plan, error) {
	plan := Plan{Source: src, staged: staged}

	if src.Dir != "" {
		if err := copyTree(src.Dir, staged); err != nil {
			return Plan{}, err
		}
	} else {
		rel, err := i.release(ctx, src)
		if err != nil {
			return Plan{}, err
		}

		asset, err := pickAsset(rel, src.Name)
		if err != nil {
			return Plan{}, err
		}
		plan.Signed = rel.signed(asset.Name)

		archive, err := i.download(ctx, asset.URL, maxArchive)
		if err != nil {
			return Plan{}, err
		}

		plan.Digest, err = i.verify(ctx, rel, asset, archive)
		if err != nil {
			return Plan{}, err
		}
		if err := unpack(asset.Name, archive, staged); err != nil {
			return Plan{}, err
		}
		if src.Version == "" {
			plan.Source.Version = rel.TagName
		}
	}

	m, err := readManifest(staged)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Plan{}, fmt.Errorf("that archive has no %s at its root, so it is not a component", ManifestName)
		}
		return Plan{}, err
	}
	if src.Name != "" && m.Name != src.Name {
		return Plan{}, fmt.Errorf("asked for the %q component but the archive contains %q", src.Name, m.Name)
	}
	if !installed(staged, m.Exec) {
		return Plan{}, fmt.Errorf("the manifest names %q as its executable, and the archive does not contain it", m.Exec)
	}

	plan.Manifest = m
	plan.Dir = filepath.Join(i.root, m.Name)
	if existing, err := readManifest(plan.Dir); err == nil {
		plan.Replaces = existing.Version
	}
	return plan, nil
}

// Commit moves a staged component into place, replacing whatever was there.
//
// The swap is a rename of one directory over another, in two steps because that is
// as atomic as a directory replacement gets: the old tree is moved aside, the new
// one moved in, and only then is the old one deleted. A crash in the middle leaves
// the component either installed or not — never half of each — and leaves at worst
// a directory called <name>.old-… that the next install cleans up.
func (i *Installer) Commit(p Plan) (Component, error) {
	if p.staged == "" {
		return Component{}, errors.New("nothing was staged")
	}
	defer func() { _ = os.RemoveAll(p.staged) }()

	if err := os.Chmod(p.staged, dirMode); err != nil {
		return Component{}, fmt.Errorf("set the mode of %s: %w", p.staged, err)
	}

	// Record where it came from, so `fft component upgrade` knows where to look and
	// `fft component info` can say whose code this is. Written into the staged tree,
	// so the manifest that lands is the manifest that was verified plus this one fact.
	m := p.Manifest
	m.Source = p.Source.String()
	if err := writeManifest(p.staged, m); err != nil {
		return Component{}, err
	}

	old := ""
	if _, err := os.Stat(p.Dir); err == nil {
		old = fmt.Sprintf("%s.old-%d", p.Dir, time.Now().UnixNano())
		if err := os.Rename(p.Dir, old); err != nil {
			return Component{}, fmt.Errorf("move the installed %s aside: %w", m.Name, err)
		}
	}

	if err := os.Rename(p.staged, p.Dir); err != nil {
		if old != "" {
			// Put back what was working. An upgrade that fails must not also uninstall.
			_ = os.Rename(old, p.Dir)
		}
		return Component{}, fmt.Errorf("install %s: %w", m.Name, err)
	}
	if old != "" {
		_ = os.RemoveAll(old)
	}

	return newComponent(m, p.Dir, false), nil
}

// Discard throws away a staged install.
func (i *Installer) Discard(p Plan) {
	if p.staged != "" {
		_ = os.RemoveAll(p.staged)
	}
}

// Remove uninstalls a component.
//
// It refuses a directory that holds no manifest, for the same reason
// `fft skill install --force` refuses a directory with no SKILL.md: the name comes
// from a shell, where a typo is one keystroke, and fft will not recursively delete
// a directory it cannot prove it put there.
func (i *Installer) Remove(name string) error {
	if i.root == "" {
		return errors.New("components are disabled: " + EnvRoot + " is set to the empty string")
	}
	if !nameRE.MatchString(name) {
		return fmt.Errorf("%q is not a component name", name)
	}

	dir := filepath.Join(i.root, name)
	if _, err := os.Stat(filepath.Join(dir, ManifestName)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("%s is not an installed component", name)
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
}

// release is the sliver of GitHub's release payload the installer needs.
type release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// asset is one downloadable file of a release.
type asset struct {
	Name string
	URL  string
}

// signed reports whether the release publishes a cosign signature for this asset.
func (r *release) signed(name string) bool {
	for _, a := range r.Assets {
		if a.Name == name+".sig" || a.Name == name+".sigstore" || a.Name == name+".pem" {
			return true
		}
	}
	return false
}

// find returns a release asset by exact name.
func (r *release) find(name string) (asset, bool) {
	for _, a := range r.Assets {
		if a.Name == name {
			return asset{Name: a.Name, URL: a.URL}, true
		}
	}
	return asset{}, false
}

// release fetches the release to install from.
func (i *Installer) release(ctx context.Context, src Source) (*release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", i.api, src.Repo)
	if src.Version != "" {
		url = fmt.Sprintf("%s/repos/%s/releases/tags/%s", i.api, src.Repo, src.Version)
	}

	body, err := i.get(ctx, url, maxRelease, "application/vnd.github+json")
	if err != nil {
		return nil, err
	}

	var rel release
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, fmt.Errorf("decode the release of %s: %w", src.Repo, err)
	}
	if len(rel.Assets) == 0 {
		return nil, fmt.Errorf("the release %s of %s has no downloadable files", rel.TagName, src.Repo)
	}
	return &rel, nil
}

// assetSuffix is what this platform's archive is called, following the convention
// GoReleaser produces and this repository's own releases already use.
func assetSuffix() string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf("_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Sprintf("_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// pickAsset chooses this platform's component archive out of a release.
//
// A release holds every platform's archive and, for a repository that ships more
// than one component, every component's. The name narrows it when the spec gave
// one; otherwise there must be exactly one candidate, and an ambiguous release is
// an error that lists what it found rather than a guess.
func pickAsset(rel *release, name string) (asset, error) {
	suffix := assetSuffix()

	if name != "" {
		want := fmt.Sprintf("fft-component-%s_%s%s", name, strings.TrimPrefix(rel.TagName, "v"), suffix)
		if a, ok := rel.find(want); ok {
			return a, nil
		}
	}

	var candidates []asset
	for _, a := range rel.Assets {
		if !strings.HasSuffix(a.Name, suffix) || !strings.HasPrefix(a.Name, "fft-component-") {
			continue
		}
		candidates = append(candidates, asset{Name: a.Name, URL: a.URL})
	}

	switch len(candidates) {
	case 1:
		return candidates[0], nil
	case 0:
		return asset{}, fmt.Errorf("the release %s has no component archive for %s/%s",
			rel.TagName, runtime.GOOS, runtime.GOARCH)
	default:
		names := make([]string, 0, len(candidates))
		for _, c := range candidates {
			names = append(names, strings.TrimSuffix(strings.TrimPrefix(c.Name, "fft-component-"), suffix))
		}
		return asset{}, fmt.Errorf("the release %s holds several components (%s): name the one you want",
			rel.TagName, strings.Join(names, ", "))
	}
}

// verify checks the archive against the release's checksums file and returns its
// digest.
//
// A release with no checksums file is refused, not installed with a shrug. The
// checksum is the only thing standing between "the release you asked for" and
// "whatever answered", and an installer that skips it when it is inconvenient
// provides no assurance at all — it just moves the failure to a day nobody is
// watching.
func (i *Installer) verify(ctx context.Context, rel *release, a asset, archive []byte) (string, error) {
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])

	checksums, ok := rel.find("checksums.txt")
	if !ok {
		return "", fmt.Errorf("the release %s publishes no checksums.txt, so %s cannot be verified", rel.TagName, a.Name)
	}

	body, err := i.get(ctx, checksums.URL, maxChecksum, "")
	if err != nil {
		return "", err
	}

	want, ok := checksumFor(string(body), a.Name)
	if !ok {
		return "", fmt.Errorf("checksums.txt of the release %s does not list %s", rel.TagName, a.Name)
	}
	if want != digest {
		return "", fmt.Errorf("%s does not match its checksum: the release says %s, the download is %s",
			a.Name, short(want), short(digest))
	}
	return digest, nil
}

// checksumFor reads one digest out of a `<sha256>  <name>` checksums file.
func checksumFor(body, name string) (string, bool) {
	for line := range strings.Lines(body) {
		digest, file, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		// The separator is two spaces in the usual format and one in some others, and
		// a leading `*` marks a binary file. None of it is worth being strict about.
		if strings.TrimLeft(strings.TrimSpace(file), "*") == name {
			return strings.ToLower(digest), true
		}
	}
	return "", false
}

// download fetches an asset.
func (i *Installer) download(ctx context.Context, url string, limit int64) ([]byte, error) {
	return i.get(ctx, url, limit, "application/octet-stream")
}

// get is every HTTP request the installer makes.
func (i *Installer) get(ctx context.Context, url string, limit int64, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "fft")

	res, err := i.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode != http.StatusOK {
		if res.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("fetch %s: not found", url)
		}
		return nil, fmt.Errorf("fetch %s: %s", url, res.Status)
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", url, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("read %s: larger than %d bytes", url, limit)
	}
	return body, nil
}

// writeManifest writes a manifest into a component directory.
func writeManifest(dir string, m Manifest) error {
	data, err := marshalManifest(m)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestName), data, fileMode); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Join(dir, ManifestName), err)
	}
	return nil
}

// safeJoin resolves a path from inside an archive against the directory it is being
// unpacked into, refusing anything that would land outside it.
//
// This is the zip-slip check, and it is not a formality: an archive is data fetched
// off the internet, and an entry called ../../.ssh/authorized_keys is a file write
// chosen by whoever published it. The checksum proves the archive is the one the
// release names; it proves nothing about what is inside.
func safeJoin(dir, name string) (string, error) {
	if name == "" || path.IsAbs(name) || strings.HasPrefix(name, "/") || strings.Contains(name, `\`) {
		return "", fmt.Errorf("the archive contains %q, which is not a relative path", name)
	}

	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("the archive contains %q, which would be written outside the component", name)
	}

	target := filepath.Join(dir, filepath.FromSlash(clean))
	// Belt and braces against a case the string check above misses on some platform:
	// ask the filesystem's own answer whether the result is still inside.
	if rel, err := filepath.Rel(dir, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("the archive contains %q, which would be written outside the component", name)
	}
	return target, nil
}

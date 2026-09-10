---
title: Install
---

# Install

**Homebrew** (macOS):

```sh
brew install Joessst-Dev/tap/fft
```

**Go** (any platform, needs Go 1.26+):

```sh
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest
```

**Binary download** — darwin/linux/windows × amd64/arm64, from the
[releases page](https://github.com/Joessst-Dev/fft-cli/releases).

The archive name carries the version, so there is no version-independent
`latest/download/` URL to fetch — name the release you want, or let
[`gh`](https://cli.github.com) pick the newest one:

```sh
gh release download --repo Joessst-Dev/fft-cli --pattern 'fft_*_linux_amd64.tar.gz'
tar xzf fft_*_linux_amd64.tar.gz
sudo mv fft /usr/local/bin/
```

By hand, substituting the release and the architecture you want:

```sh
VERSION=0.7.0
curl -sSLO "https://github.com/Joessst-Dev/fft-cli/releases/download/v$VERSION/fft_${VERSION}_linux_amd64.tar.gz"
tar xzf "fft_${VERSION}_linux_amd64.tar.gz"
sudo mv fft /usr/local/bin/
```

Archives are checksummed, SBOM'd, and signed with [cosign](https://docs.sigstore.dev/)
keylessly — see [Verifying a download](#verifying-a-download).

Confirm it worked:

```sh
fft version
```

fft tells you when a newer release exists (at most once a day, on stderr, never in your
way), and names the upgrade command for how it was installed. Set
`FFT_NO_UPDATE_CHECK=1` to turn that off, or `FFT_INSTALL_METHOD` to
`homebrew`/`scoop`/`winget`/`go` if it guesses wrong.

## Verifying a download

Releases are signed keylessly: there is no private key, and the signature is bound to
this repository and the exact workflow that produced it, in Sigstore's public
transparency log.

```sh
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp 'https://github.com/Joessst-Dev/fft-cli/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt

sha256sum --check checksums.txt --ignore-missing
```

The signature ships as a single Sigstore bundle (`checksums.txt.bundle`) — cert,
signature and transparency-log entry in one file.

The Homebrew cask strips the `com.apple.quarantine` attribute on install, so macOS
Gatekeeper does not second-guess the binary — the sha256 and cosign chain above are the
substitute for notarization. That is standard for an unnotarized tool; if you would rather
Gatekeeper vet it, download the archive in a browser and verify it by hand instead.

---
title: Install
---

# Install

**Homebrew** (macOS):

```sh
brew install Joessst-Dev/tap/fft
```

**Scoop** (Windows):

```powershell
scoop bucket add joessst https://github.com/Joessst-Dev/scoop-bucket
scoop install joessst/fft
```

`joessst/fft` names the bucket as well as the app, so another bucket's `fft` can never be
the one installed. Scoop checks the archive against the manifest's sha256 — the same
hash `checksums.txt` carries — before it unpacks anything.

**Go** (any platform, needs Go 1.26+):

```sh
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest
```

**Binary download** — darwin/linux/windows × amd64/arm64, from the
[releases page](https://github.com/Joessst-Dev/fft-cli/releases). Archive names carry the
version (`fft_0.8.0_linux_amd64.tar.gz`), so there is no stable "latest" filename to
download blind — take the version from the releases page, or let `gh` resolve it:

```sh
VERSION=0.8.0
curl -sSL "https://github.com/Joessst-Dev/fft-cli/releases/download/v$VERSION/fft_${VERSION}_linux_amd64.tar.gz" | tar xz
sudo mv fft /usr/local/bin/

# or, always the newest release:
gh release download --repo Joessst-Dev/fft-cli --pattern 'fft_*_linux_amd64.tar.gz' --output - | tar xz
```

**Windows without Scoop**: unzip the archive and put `fft.exe` somewhere on `PATH`:

```powershell
$Version = "0.8.0"
Invoke-WebRequest "https://github.com/Joessst-Dev/fft-cli/releases/download/v$Version/fft_${Version}_windows_amd64.zip" -OutFile fft.zip
Expand-Archive fft.zip -DestinationPath $env:LOCALAPPDATA\Programs\fft -Force
$env:PATH += ";$env:LOCALAPPDATA\Programs\fft"
```

Use `windows_arm64` on an Arm device. That `$env:PATH` line lasts for the session only —
add the directory under *Edit environment variables for your account* to make it stick.
SmartScreen may warn on the download: the binaries are not Authenticode-signed, and the
checksum and cosign chain below are what vouches for them instead.

Archives are checksummed, SBOM'd, and signed with [cosign](https://docs.sigstore.dev/)
keylessly — see [Verifying a download](#verifying-a-download).

Confirm it worked:

```sh
fft version
```

fft tells you when a newer release exists (at most once a day, on stderr, never in your
way). Set `FFT_NO_UPDATE_CHECK=1` to turn that off.

## Upgrading

`fft update check` only *reports* a newer release — nothing in fft replaces its own
binary. How you upgrade is how you installed:

```sh
brew upgrade fft                                          # Homebrew
scoop update; scoop update fft                            # Scoop
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest  # Go
```

The bare `scoop update` refreshes the buckets first. Without it, `scoop update fft` can
skip the refresh when the buckets were last pulled recently, and then report the old
fft as current right after fft said a new one was out.

A binary install is upgraded by repeating the download above over the old `fft.exe` or
`fft`. On Windows, close any running `fft` first — `fft tui` especially — because Windows
locks a running executable and the copy fails with a sharing violation rather than a
clear error.

An installed [agent skill](/guide/agents) is a copy too, and still describes the old
release: `fft skill install` brings it up to date, and fft reminds you until it has.

[Components](/guide/components) are upgraded separately: a new fft does not carry new
component binaries, and `fft component upgrade <name>` refetches one from wherever it was
installed from.

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

On Windows, `Get-FileHash fft.zip -Algorithm SHA256` gives the hash to compare against the
matching line of `checksums.txt` by eye.

The signature ships as a single Sigstore bundle (`checksums.txt.bundle`) — cert,
signature and transparency-log entry in one file.

The Homebrew cask strips the `com.apple.quarantine` attribute on install, so macOS
Gatekeeper does not second-guess the binary — the sha256 and cosign chain above are the
substitute for notarization. That is standard for an unnotarized tool; if you would rather
Gatekeeper vet it, download the archive in a browser and verify it by hand instead.

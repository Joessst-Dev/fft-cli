---
title: Install
---

# Install

## macOS

```sh
brew install Joessst-Dev/tap/fft
```

Upgrades are `brew upgrade fft`.

## Windows

With [Scoop](https://scoop.sh):

```powershell
scoop bucket add joessst https://github.com/Joessst-Dev/scoop-bucket
scoop install fft
```

With [WinGet](https://learn.microsoft.com/windows/package-manager/), which ships with
Windows 10 and 11:

```powershell
winget install Joessst-Dev.fft
```

Either one puts `fft` on your `PATH` — Scoop through its shim directory
(`~\scoop\shims`), WinGet through `%LOCALAPPDATA%\Microsoft\WinGet\Links`. Both of
those were added to `PATH` when the package manager itself was set up, not by this
install, so **open a new terminal** before running `fft version`: a shell that was
already open still has the old `PATH`.

Upgrades are `scoop update fft` and `winget upgrade Joessst-Dev.fft` respectively. fft
tells you which one applies to your install.

## Any platform, with Go

Needs Go 1.26+.

```sh
go install github.com/Joessst-Dev/fft-cli/cmd/fft@latest
```

Upgrades are the same command again.

## Binary download

darwin/linux/windows × amd64/arm64, from the
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

On Windows, in PowerShell — download, check the hash, then unpack. The hash check is
the point of doing it by hand rather than through a package manager, which would do it
for you:

```powershell
$Version = '0.7.0'
$Arch    = 'amd64'                     # or 'arm64'
$Zip     = "fft_${Version}_windows_${Arch}.zip"
$Base    = "https://github.com/Joessst-Dev/fft-cli/releases/download/v$Version"

Invoke-WebRequest "$Base/$Zip"          -OutFile $Zip
Invoke-WebRequest "$Base/checksums.txt" -OutFile checksums.txt

$expected = ((Select-String -Path checksums.txt -SimpleMatch $Zip).Line -split '\s+')[0]
$actual   = (Get-FileHash $Zip -Algorithm SHA256).Hash.ToLower()
if ($actual -ne $expected) { throw "checksum mismatch for $Zip" }

$Dest = "$env:LOCALAPPDATA\Programs\fft"
Expand-Archive $Zip -DestinationPath $Dest -Force
```

Then put it on your `PATH`. This edits your *user* environment, so it survives a reboot
and leaves other accounts alone:

```powershell
$Dest = "$env:LOCALAPPDATA\Programs\fft"
$Path = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($Path -notlike "*$Dest*") {
  [Environment]::SetEnvironmentVariable('Path', "$Path;$Dest", 'User')
}
```

Open a new terminal for that to take effect.

Archives are checksummed, SBOM'd, and signed with [cosign](https://docs.sigstore.dev/)
keylessly — see [Verifying a download](#verifying-a-download).

## Confirm it worked

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

That last line is Unix-only; the PowerShell equivalent is the `Get-FileHash` comparison
in [Binary download](#binary-download) above.

The signature ships as a single Sigstore bundle (`checksums.txt.bundle`) — cert,
signature and transparency-log entry in one file.

On Windows, SmartScreen may warn about a binary it has not seen downloaded often enough
to have formed an opinion about. That is a reputation signal, not a verdict on the file:
the sha256 and the cosign chain above are what actually establish that the archive is
the one this repository's release workflow built. Installing through Scoop or WinGet
avoids the prompt, because neither goes through the browser download path SmartScreen
inspects.

On macOS, the Homebrew cask strips the `com.apple.quarantine` attribute on install, so
Gatekeeper does not second-guess the binary — the sha256 and cosign chain above are the
substitute for notarization. That is standard for an unnotarized tool; if you would rather
Gatekeeper vet it, download the archive in a browser and verify it by hand instead.

# Changesets

Every pull request that changes what `fft` does adds a changeset: run `npx changeset`, pick the bump, and describe the change from the point of view of someone running the CLI. The text becomes the changelog entry and the GitHub release notes.

Pick the bump by what a user of the CLI sees:

- **patch**: a bug fix, a newly synced spec operation, a clearer message.
- **minor**: a new command, flag or output field.
- **A breaking change** (a removed or renamed command or flag, a changed exit code, a changed `-o json` shape) is a **minor** while `fft` is `0.x`, and the text says what breaks. Picking major would release 1.0.0.

Docs-, CI- and tooling-only pull requests don't need one; label them `no-changeset`. Dependabot pull requests are exempt automatically.

Merged changesets collect in a "Version packages" pull request. Merging that pull request tags `vX.Y.Z`, and the release workflow builds and publishes it.

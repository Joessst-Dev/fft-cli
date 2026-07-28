# Not a from-source build: GoReleaser has already produced the static, reproducible,
# CGO_ENABLED=0 binaries (see .goreleaser.yaml) and passes them into this build context,
# so the image just carries them. distroless/static is the right base for such binaries —
# no libc, no shell, no package manager — but it does ship the CA certificates the update
# check's HTTPS call needs. The version is already stamped into fft via ldflags, so
# `fft version` reports the release tag with nothing set here.
#
# Pinned by digest, not just the :nonroot tag, so the signed release image is
# reproducible — whatever :nonroot pointed at on build day cannot drift underneath
# it. Dependabot's docker ecosystem bumps this pin the same as any dependency.
FROM gcr.io/distroless/static:nonroot@sha256:f7f8f729987ad0fdf6b05eeeae94b26e6a0f613bdf46feea7fc40f7bd72953e6

# dockers_v2 stages every platform's build under <os>/<arch>/ in one build context, so
# select this platform's with the TARGETOS/TARGETARCH that buildx sets per target. fft
# lands at the context root; each component binary at bin/<name>, because that is the
# `binary:` path its build declares — the same path its manifest's `exec:` names.
ARG TARGETOS
ARG TARGETARCH

COPY ${TARGETOS}/${TARGETARCH}/fft /usr/bin/fft

# The first-party components, assembled into a component root the image bakes in: each
# one a directory of its committed manifest and its binary, exactly as `fft component
# install` would lay it out on disk. FFT_COMPONENT_DIR (below) points fft here, so
# `fft emulator` finds bin/fft-emulator without anything being installed at run time —
# which is what keeps `docker run …/fft emulator --host 0.0.0.0` unchanged after the
# emulator moved out of the fft binary.
COPY components/emulator/component.yaml                    /opt/fft/components/emulator/component.yaml
COPY ${TARGETOS}/${TARGETARCH}/bin/fft-emulator           /opt/fft/components/emulator/bin/fft-emulator
COPY components/emulator-pubsub/component.yaml            /opt/fft/components/emulator-pubsub/component.yaml
COPY ${TARGETOS}/${TARGETARCH}/bin/fft-emulator-pubsub    /opt/fft/components/emulator-pubsub/bin/fft-emulator-pubsub
COPY components/emulator-servicebus/component.yaml        /opt/fft/components/emulator-servicebus/component.yaml
COPY ${TARGETOS}/${TARGETARCH}/bin/fft-emulator-servicebus /opt/fft/components/emulator-servicebus/bin/fft-emulator-servicebus

# The nonroot user (uid 65532) has no writable $HOME, so the component root is a baked,
# read-only path chosen here rather than left to default under ~/.local/share. The
# components are pre-installed, so nothing writes to it at run time.
ENV FFT_COMPONENT_DIR=/opt/fft/components

# The emulator binds 8080, above 1024, so the nonroot user can serve it without extra
# privileges. The ENTRYPOINT is fft, so `docker run …/fft emulator --host 0.0.0.0` and a
# compose `command: ["emulator", …]` both append their args to it.
EXPOSE 8080

ENTRYPOINT ["/usr/bin/fft"]

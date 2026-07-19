# Not a from-source build: GoReleaser has already produced the static, reproducible,
# CGO_ENABLED=0 binary (see .goreleaser.yaml) and passes it into this build context, so
# the image just carries it. distroless/static is the right base for such a binary — no
# libc, no shell, no package manager — but it does ship the CA certificates the update
# check's HTTPS call needs. The version is already stamped into the binary via ldflags, so
# `fft version` reports the release tag with nothing set here.
FROM gcr.io/distroless/static:nonroot

# dockers_v2 stages every platform's binary under <os>/<arch>/ in one build context, so
# select this platform's with the TARGETOS/TARGETARCH that buildx sets per target. The
# ENTRYPOINT is the binary, so `docker run …/fft emulator --host 0.0.0.0` and a compose
# `command: ["emulator", …]` both append their args to it. The emulator binds 8080, above
# 1024, so the distroless nonroot user (uid 65532) can serve it without extra privileges.
ARG TARGETOS
ARG TARGETARCH
COPY ${TARGETOS}/${TARGETARCH}/fft /usr/bin/fft

EXPOSE 8080

ENTRYPOINT ["/usr/bin/fft"]

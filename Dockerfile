# Not a from-source build: GoReleaser has already produced the static, reproducible,
# CGO_ENABLED=0 binary (see .goreleaser.yaml) and passes it into this build context, so
# the image just carries it. distroless/static is the right base for such a binary — no
# libc, no shell, no package manager, but it does ship CA certificates and tzdata, which
# the update check's HTTPS call needs. The version is already stamped into the binary via
# ldflags, so `fft version` reports the release tag with nothing set here.
FROM gcr.io/distroless/static:nonroot

# ENTRYPOINT is the binary, so `docker run …/fft emulator --host 0.0.0.0` and a compose
# `command: ["emulator", …]` both append their args to it. The emulator binds 8080, above
# 1024, so the distroless nonroot user (uid 65532) can serve it without extra privileges.
COPY fft /usr/bin/fft

EXPOSE 8080

ENTRYPOINT ["/usr/bin/fft"]

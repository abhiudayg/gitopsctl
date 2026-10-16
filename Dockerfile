# Build stage is handled by GoReleaser
FROM alpine:3.21@sha256:ce64758a109eb420d874a118f87920e625e12d3634e03b4a5573fd9f6e5d3507

RUN apk add --no-cache ca-certificates git openssh tzdata

# Create a non-root user
RUN addgroup -S gitopsctl && adduser -S gitopsctl -G gitopsctl
USER gitopsctl

WORKDIR /app

# GoReleaser will copy the built binary into the image
ARG TARGETPLATFORM
COPY ${TARGETPLATFORM}/gitopsctl /usr/local/bin/gitopsctl

ENTRYPOINT ["/usr/local/bin/gitopsctl"]
CMD ["start"]

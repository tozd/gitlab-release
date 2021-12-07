# This Dockerfile requires DOCKER_BUILDKIT=1 to be build.
# We do not use syntax header so that we do not have to wait
# for the Dockerfile frontend image to be pulled.
FROM golang:1.17-alpine3.14 AS build

RUN apk --update add make git gcc musl-dev ca-certificates tzdata && \
 adduser -D -H -g "" -s /sbin/nologin -u 1000 user
COPY . /go/src/gitlab-release
WORKDIR /go/src/gitlab-release
# We want Docker image for build timestamp label to match the one in
# the binary so we take a timestamp once outside and pass it in.
ARG BUILD_TIMESTAMP
RUN \
 BUILD_TIMESTAMP=$BUILD_TIMESTAMP make build-static && \
 mv gitlab-release /go/bin/gitlab-release

FROM alpine:3.14 AS debug
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group
COPY --from=build /go/bin/gitlab-release /
USER user:user
ENTRYPOINT ["/gitlab-release"]

FROM scratch AS production
RUN --mount=from=busybox:1.34,src=/bin/,dst=/bin/ ["/bin/mkdir", "-m", "1755", "/tmp"]
COPY --from=build /etc/services /etc/services
COPY --from=build /etc/protocols /etc/protocols
# The rest is the same as for the debug image.
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /etc/passwd /etc/passwd
COPY --from=build /etc/group /etc/group
COPY --from=build /go/bin/gitlab-release /
USER user:user
ENTRYPOINT ["/gitlab-release"]

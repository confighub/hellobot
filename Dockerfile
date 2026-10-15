# Copyright (C) ConfigHub, Inc.
# SPDX-License-Identifier: MIT

FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /hellobot .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /hellobot /hellobot
USER nonroot
ENTRYPOINT ["/hellobot"]

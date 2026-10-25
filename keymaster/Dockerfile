FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /keymaster ./cmd/keymaster

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /keymaster /keymaster
USER nonroot
EXPOSE 8080 8081
ENTRYPOINT ["/keymaster"]

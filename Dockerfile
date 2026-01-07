FROM golang:1.22-alpine AS build

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod ./
COPY main.go ./

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/tg-backend-bot .

FROM scratch
COPY --from=build /out/tg-backend-bot /tg-backend-bot
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

ENTRYPOINT ["/tg-backend-bot"]

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD ["/tg-backend-bot", "--healthcheck"]

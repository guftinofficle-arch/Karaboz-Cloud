FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache ca-certificates
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/karaboz-cloud ./cmd/api
FROM alpine:3.21
RUN apk add --no-cache ca-certificates && adduser -D -H -u 10001 app
COPY --from=build /out/karaboz-cloud /usr/local/bin/karaboz-cloud
USER 10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/karaboz-cloud"]

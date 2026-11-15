FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/ ./cmd/api ./cmd/mock-access

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=build /out/api /out/mock-access /usr/local/bin/
USER app
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/api"]

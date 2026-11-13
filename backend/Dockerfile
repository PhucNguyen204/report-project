FROM golang:1.26.11-alpine@sha256:ae52b5bc6ed045a9ed26ab03dc19327a802a9cc9aa8b27ed57f5c6b60579b0bb AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dicho ./cmd/server && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dicho-keygen ./cmd/keygen && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dicho-migrate ./cmd/migrate

FROM alpine:3.23@sha256:469c9ea410b9215eaee7976ba6c0639039b444be8e5d638f7abbada189971eb2
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 dicho && adduser -S -u 10001 -G dicho dicho
COPY --from=build /out/dicho /out/dicho-keygen /out/dicho-migrate /usr/local/bin/
USER 10001:10001
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/dicho"]

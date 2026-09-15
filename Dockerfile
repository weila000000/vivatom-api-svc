FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/vivatom-api ./cmd/api

FROM alpine:3.22
RUN addgroup -S vivatom && adduser -S vivatom -G vivatom
WORKDIR /app
COPY --from=build /out/vivatom-api /usr/local/bin/vivatom-api
RUN mkdir /app/data && chown vivatom:vivatom /app/data
USER vivatom
EXPOSE 8080
ENTRYPOINT ["vivatom-api"]

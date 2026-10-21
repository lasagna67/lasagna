FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod main.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -o /out/bki-mock .



FROM alpine:3.24

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=build /out/bki-mock ./bki-mock
COPY testdata/bki.json ./testdata/

USER app:app

EXPOSE 8082

ENTRYPOINT ["/app/bki-mock"]

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod main.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -o /out/registry-mock .



FROM alpine:3.24

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=build /out/registry-mock ./registry-mock
COPY testdata/egrip.json testdata/egrul.json ./testdata/

USER app:app

EXPOSE 8081

ENTRYPOINT ["/app/registry-mock"]

FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod main.go ./
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -o /out/esign-mock .



FROM alpine:3.24

RUN addgroup -S app && adduser -S -G app app

WORKDIR /app

COPY --from=build /out/esign-mock ./esign-mock
COPY testdata/certificates.json ./testdata/

USER app:app

EXPOSE 8084

ENTRYPOINT ["/app/esign-mock"]

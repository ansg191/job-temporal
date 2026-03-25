FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/trigger-server ./cmd/trigger-server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/trigger-server /trigger-server

EXPOSE 8090

ENTRYPOINT ["/trigger-server"]

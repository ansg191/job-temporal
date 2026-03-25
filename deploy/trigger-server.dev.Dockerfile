FROM golang:1.25-bookworm

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

EXPOSE 8090

CMD ["go", "run", "./cmd/trigger-server"]

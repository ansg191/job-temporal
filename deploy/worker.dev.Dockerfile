FROM golang:1.25-bookworm

ARG TYPST_VERSION=0.13.1

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl fontconfig xz-utils fonts-dejavu-core fonts-liberation2 \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fL "https://github.com/typst/typst/releases/download/v${TYPST_VERSION}/typst-x86_64-unknown-linux-musl.tar.xz" -o /tmp/typst.tar.xz \
    && tar -xJf /tmp/typst.tar.xz -C /tmp \
    && mv /tmp/typst-x86_64-unknown-linux-musl/typst /usr/local/bin/typst \
    && chmod +x /usr/local/bin/typst \
    && rm -rf /tmp/typst.tar.xz /tmp/typst-x86_64-unknown-linux-musl

RUN mkdir -p /opt/custom-fonts \
    && fc-cache -f

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY . .

CMD ["go", "run", "./cmd/worker"]

FROM golang:1.15.8 AS builder

COPY . /app
WORKDIR /app
ENV GOPROXY=http://goproxy.i.brainpp.cn
ENV GOSUMDB=off

RUN CGO_ENABLED=0 go build -x -o /usr/local/bin/server ./server
RUN CGO_ENABLED=0 go build -x -o /usr/local/bin/client ./client

FROM ubuntu:18.04

COPY --from=builder /usr/local/bin/server /usr/local/bin/
COPY --from=builder /usr/local/bin/client /usr/local/bin/

RUN sed -i -e "s/archive.ubuntu.com/mirrors.i.brainpp.cn/g" /etc/apt/sources.list && \
    sed -i -e "s/security.ubuntu.com/mirrors.i.brainpp.cn/g" /etc/apt/sources.list && \
    rm -rf /var/lib/apt/lists/* && \
    apt-get clean && apt-get update && \
    apt-get install -y locales vim

RUN addgroup --gid "10250" "demo" && \
    useradd --uid "10250" --gid "10250" -G sudo,video -m "demo" -s /bin/bash

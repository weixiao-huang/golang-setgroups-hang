ARG BUILD_IMAGE=golang:1.15.8
FROM $BUILD_IAMGE AS builder

COPY . /app
WORKDIR /app
ENV GOPROXY=http://goproxy.i.brainpp.cn
ENV GOSUMDB=off

RUN CGO_ENABLED=0 go build -x -o /usr/local/bin/server ./server
RUN CGO_ENABLED=0 go build -x -o /usr/local/bin/client ./client

FROM ubuntu:18.04

COPY --from=builder /usr/local/bin/server /usr/local/bin/
COPY --from=builder /usr/local/bin/client /usr/local/bin/

RUN addgroup --gid "10250" "demo" && \
    useradd --uid "10250" --gid "10250" -G sudo,video -m "demo" -s /bin/bash

FROM ubuntu:24.04

RUN apt-get -y update && \
    apt-get -y --no-install-recommends upgrade && \
    apt-get -y --no-install-recommends install unminimize && \
    echo y | unminimize && \
    apt-get -y --no-install-recommends install git gh npm curl wget vim golang-go build-essential openssl && \
    npm install -g @anthropic-ai/claude-code

ENV GOPATH=/go
ENV PATH=$GOPATH/bin:$PATH

RUN go install github.com/go-delve/delve/cmd/dlv@latest && \
    go install golang.org/x/tools/gopls@latest && \
    go install github.com/golang/mock/mockgen@latest

# syntax=docker/dockerfile:1
FROM golang:1.16 AS builder
WORKDIR /go/src/github.com/eljojo/nara/
COPY . /go/src/github.com/eljojo/nara
RUN go get -d -v
ARG opts
RUN env ${opts} scripts/build.sh

FROM alpine:latest  
RUN apk --no-cache add ca-certificates tzdata
WORKDIR /root/
COPY --from=builder /go/src/github.com/eljojo/nara/build/nara .
CMD ["./nara"]  

FROM golang:1.24.2 as builder
COPY go.mod go.sum /go/src/github.com/oybek/br/
WORKDIR /go/src/github.com/oybek/br
RUN go mod download
COPY . /go/src/github.com/oybek/br
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o build/br github.com/oybek/br

FROM alpine/curl
RUN apk add --no-cache ca-certificates && update-ca-certificates
COPY --from=builder /go/src/github.com/oybek/br/build/br /usr/bin/br
EXPOSE 8080 8080
ENTRYPOINT ["/usr/bin/br"]

FROM golang:1.16
COPY gomoduleproxy /gomoduleproxy
ENTRYPOINT ["/gomoduleproxy", "server"]

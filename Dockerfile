FROM golang:1.14
COPY gomoduleproxy /gomoduleproxy
ENTRYPOINT ["/gomoduleproxy", "server"]

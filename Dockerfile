FROM golang:1.17
COPY gomoduleproxy /gomoduleproxy
ENTRYPOINT ["/gomoduleproxy", "server"]

FROM golang:1.20
COPY gomoduleproxy /gomoduleproxy
ENTRYPOINT ["/gomoduleproxy", "server"]

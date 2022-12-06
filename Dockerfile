FROM golang:1.18
COPY gomoduleproxy /gomoduleproxy
ENTRYPOINT ["/gomoduleproxy", "server"]

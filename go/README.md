# Run unit tests
```bash
go test -coverpkg=./... -coverprofile=coverage.out ./...; go tool cover -html=coverage.out
```

See [../scripts/main.sh](../scripts/main.sh) for various other scripts
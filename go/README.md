# Run unit tests
```bash
go test -coverpkg=./... -coverprofile=coverage.out ./...; go tool cover -html=coverage.out
```

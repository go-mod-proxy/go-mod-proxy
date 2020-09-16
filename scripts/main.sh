#!/usr/bin/env bash
set -euo pipefail

readonly CONTAINING_DIR=$(unset CDPATH && cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
readonly GO_VERSION_REGEXP='^([^ ]+ ){2}go([0-9]+)\.([0-9]+)([\. ]|$)'

get_go_version() {
  local S
  S=$(go version)
  [[ ${S} =~ ${GO_VERSION_REGEXP} ]] || {
    1>&2 echo "command 'go version' has unexpected stdout: $S"
    return 1
  }
  MAJOR=${BASH_REMATCH[2]}
  MINOR=${BASH_REMATCH[3]}
  [[ ${MAJOR} -eq 1 ]] || {
    1>&2 echo "unsupported go major version ${MAJOR}"
    return 1
  }
  echo "${MINOR}"
}

build() {
  local GO_VERSION_MINOR
  GO_VERSION_MINOR=$(get_go_version)
  if [[ ${GO_VERSION_MINOR} -ne 14 ]]; then
    1>&2 echo "unsupported go version 1.${GO_VERSION_MINOR}"
    return 1
  fi
  pushd "${CONTAINING_DIR}"/../go 1>/dev/null
  CGO_ENABLED=0 GOARCH=amd64 GOOS=linux go build \
    -ldflags='-w -s' \
    -o "${CONTAINING_DIR}"/../gomoduleproxy \
    ./cmd
  popd 1>/dev/null

  pushd "${CONTAINING_DIR}"/.. 1>/dev/null
  docker build --tag=jbrekelmans/go-module-proxy:latest .
  popd 1>/dev/null
}

run-go() {
  local GO_VERSION_MINOR
  GO_VERSION_MINOR=$(get_go_version)
  if [[ ${GO_VERSION_MINOR} -ne 14 ]]; then
    1>&2 echo "unsupported go version 1.${GO_VERSION_MINOR}"
    return 1
  fi
  pushd "${CONTAINING_DIR}"/../go 1>/dev/null
  export GOOGLE_APPLICATION_CREDENTIALS=${CONTAINING_DIR}/scratch-playground-35817e60db32.json
  go run ./cmd \
    server \
    --config-file="${CONTAINING_DIR}"/config.yaml \
    --log-level=trace \
    --port=3000 \
    --credential-helper-port=3001
  popd 1>/dev/null
}

test() {
  local GO_VERSION_MINOR
  GO_VERSION_MINOR=$(get_go_version)
  if [[ ${GO_VERSION_MINOR} -ne 14 ]]; then
    1>&2 echo "unsupported go version 1.${GO_VERSION_MINOR}"
    return 1
  fi
  pushd "${CONTAINING_DIR}"/../go 1>/dev/null
  go test ./...
  popd 1>/dev/null
}

main() {
  case "${1-build}" in
    test)
      test
      ;;
    build)
      build
      ;;
    run-docker)
      build
      docker run \
        -v="${CONTAINING_DIR}"/config.yaml:/mnt/config.yaml \
        -p='3000:3000' \
        jbrekelmans/go-module-proxy:latest \
        --config-file=/mnt/config.yaml \
        --log-level=trace \
        --port=3000 \
        --credential-helper-port=3001
      ;;
    run-go)
      run-go
      ;;
    run-go-tests)
      curl --fail \
        -v \
        -H 'Content-Type: application/json' \
        --data '{"user":"x","password":"test"}' \
        --output "${CONTAINING_DIR}"/responsebody \
        http://127.0.0.1:3000/auth/userpassword
      ACCESS_TOKEN=$(jq -r '.access_token' "${CONTAINING_DIR}"/responsebody)

      # Public module
      MODULE_PATH='github.com/sirupsen/logrus'
      # @v/list
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@v/list)
      echo "${STATUS_CODE}"
      VERSIONS=$(cat "${CONTAINING_DIR}"/responsebody)
      VERSIONS=${VERSIONS%$'\n'}
      VERSIONS=${VERSIONS##*$'\n'}
      # .info
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@v/"${VERSIONS}".info)
      echo "${STATUS_CODE}"
      cat "${CONTAINING_DIR}"/responsebody
      # @latest
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@latest)
      echo "${STATUS_CODE}"
      cat "${CONTAINING_DIR}"/responsebody

      # 404 test
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@v/v1234.0.0.info) || true 
      echo "${STATUS_CODE}"
      cat "${CONTAINING_DIR}"/responsebody

      # Private module (github.com)
      MODULE_PATH='github.com/go-mod-proxy/testrepo'
      # @v/list
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@v/list)
      echo "${STATUS_CODE}"
      cat "${CONTAINING_DIR}"/responsebody
      VERSIONS=$(cat "${CONTAINING_DIR}"/responsebody)
      VERSIONS=${VERSIONS%$'\n'}
      VERSIONS=${VERSIONS##*$'\n'}
      # .info
      STATUS_CODE=$(curl --fail -s -w "%{http_code}" -H 'Authorization: Bearer '"${ACCESS_TOKEN}" --output "${CONTAINING_DIR}"/responsebody http://127.0.0.1:3000/"${MODULE_PATH}"/@v/"${VERSIONS}".info)
      echo "${STATUS_CODE}"
      cat "${CONTAINING_DIR}"/responsebody
      ;;
    *)
      1>&2 echo "unexpected arg build"
      exit 1
      ;;
  esac
}

main "$@"
